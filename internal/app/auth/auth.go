// Package auth implements JWT authentication and revocation in Redis.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud-tariffs-backend/internal/app/currentuser"
	"cloud-tariffs-backend/internal/app/ds"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ЛР4. Здесь находится общая логика входа по JWT и проверки прав.
// Role — название роли пользователя. От роли зависит доступ к методам сервера.
type Role string

// Создатель и модератор — две роли, которые понимает наш сервер.
// В этой работе обе роли изменяют только свои тарифы. issuer — имя издателя JWT.
const (
	Creator   Role = "creator"
	Moderator Role = "moderator"
	issuer         = "cloud-tariffs"
)

// Различаем три причины отказа: плохой JWT, отозванный JWT и недоступный Redis.
// Это позволяет вернуть подходящий ответ, а не одну ошибку для всех случаев.
var ErrInvalidToken = errors.New("недействительный токен")
var ErrRevokedToken = errors.New("токен отозван")
var ErrBlacklistUnavailable = errors.New("Redis blacklist недоступен")

// Claims — данные внутри токена. Это обычные сведения, а не зашифрованный пароль.
// RegisteredClaims добавляет стандартные поля: кто выдал токен, кому, когда и до какого времени.
// UserID и Role добавлены для нашего приложения. Подпись защищает их от изменения.
type Claims struct {
	jwt.RegisteredClaims
	UserID uint `json:"user_id"`
	Role   Role `json:"role"`
}

// Blacklist — список токенов, которыми больше нельзя пользоваться после выхода.
// Интерфейс описывает две нужные операции. Само хранилище можно заменить, не меняя JWT-сервис.
// context.Context позволяет ограничить ожидание и отменить операцию.
type Blacklist interface {
	Exists(context.Context, string) (bool, error)
	Revoke(context.Context, string, time.Duration) error
}

// Эта реализация списка отозванных токенов использует Redis.
// Client — готовое подключение, через которое отправляем команды Redis.
type RedisBlacklist struct{ Client *redis.Client }

// Из JWT получаем название ключа Redis. Сам длинный токен в ключ не записываем.
func BlacklistKey(token string) string {
	// SHA-256 делает из токена отпечаток фиксированной длины.
	// Это не подпись JWT и не хеш пароля: здесь хеш нужен только для имени записи Redis.
	hash := sha256.Sum256([]byte(token))
	// hex превращает байты отпечатка в строку из цифр и букв a–f.
	// Префикс позволяет отличать наши записи от других данных Redis.
	return "cloud-tariffs:jwt:blacklist:" + hex.EncodeToString(hash[:])
}

// Проверяем, есть ли запись об отзыве именно этого токена.
func (b RedisBlacklist) Exists(ctx context.Context, token string) (bool, error) {
	// Команда EXISTS возвращает количество найденных ключей: здесь 0 или 1.
	// Ошибка соединения возвращается отдельно; её нельзя считать отсутствием записи.
	n, err := b.Client.Exists(ctx, BlacklistKey(token)).Result()
	return n > 0, err
}

// Записываем токен в blacklist. ttl — сколько времени хранить запись.
func (b RedisBlacklist) Revoke(ctx context.Context, token string, ttl time.Duration) error {
	// Если срок JWT уже закончился, хранить его в blacklist больше не нужно.
	if ttl <= 0 {
		return nil
	}
	// SET сохраняет значение "1" и срок жизни ключа.
	// После истечения ttl Redis удалит ключ автоматически. Он совпадает с остатком срока JWT.
	return b.Client.Set(ctx, BlacklistKey(token), "1", ttl).Err()
}

// Service объединяет всё для JWT: секрет подписи, срок действия и blacklist.
// Секрет остаётся на сервере. Клиенту его не передаём.
type Service struct {
	secret    []byte
	ttl       time.Duration
	blacklist Blacklist
}

// Создаём JWT-сервис один раз при запуске приложения.
// Если настройки неправильные, сразу останавливаем запуск с понятной ошибкой.
func New(secret string, ttl time.Duration, blacklist Blacklist) (*Service, error) {
	// Секрет должен содержать не менее 32 байт. Его значение берётся из JWT_SECRET.
	// Для случайного секрета в инструкции используется openssl rand.
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET должен содержать не менее 32 байт")
	}
	// Не разрешаем бессмысленный или слишком большой срок токена: от секунды до суток.
	if ttl < time.Second || ttl > 24*time.Hour {
		return nil, fmt.Errorf("JWT_EXPIRES_IN: от 1s до 24h")
	}
	// Без blacklist мы не могли бы реально отозвать JWT при выходе.
	if blacklist == nil {
		return nil, fmt.Errorf("blacklist не настроен")
	}
	return &Service{secret: []byte(secret), ttl: ttl, blacklist: blacklist}, nil
}

// Отдаём настроенный срок JWT. Обработчик входа переводит его в секунды для ответа.
func (s *Service) TTL() time.Duration { return s.ttl }

// Выпускаем JWT после того, как обработчик уже проверил логин и пароль.
func (s *Service) Issue(user ds.User) (string, *Claims, error) {
	// Новая роль не берётся из запроса клиента: читаем существующее поле пользователя.
	// Если is_moderator=true, выдаём moderator; иначе creator.
	role := Creator
	if user.IsModerator {
		role = Moderator
	}
	// Запоминаем время выпуска: от него отсчитывается окончание срока JWT.
	now := time.Now()
	// Issuer (iss) — кто выпустил токен; Subject (sub) — ID пользователя строкой.
	// ID (jti) — уникальное имя этого конкретного токена. При каждом входе оно новое.
	// IssuedAt (iat) — время выдачи; ExpiresAt (exp) — время окончания действия.
	// UserID и Role — сведения, которыми воспользуются обработчики запросов.
	claims := &Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, Subject: strconv.FormatUint(uint64(user.UserID), 10), ID: uuid.NewString(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}, UserID: user.UserID, Role: role}
	// HS256 вычисляет подпись с помощью одного секретного ключа сервера.
	// Если кто-то изменит ID или роль внутри JWT, проверка подписи не пройдёт.
	// Содержимое JWT при этом не шифруется; пароль в токен не помещаем.
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	return token, claims, err
}

// Проверяем JWT, который клиент прислал в очередном запросе.
// raw — полная строка токена; ctx относится к текущему HTTP-запросу.
func (s *Service) Validate(ctx context.Context, raw string) (*Claims, error) {
	// Пустой и чрезмерно длинный токен сразу отклоняем.
	if raw == "" || len(raw) > 4096 {
		return nil, ErrInvalidToken
	}
	// Подготавливаем структуру, куда библиотека прочитает данные из JWT.
	claims := &Claims{}
	// Библиотека разбирает JWT и сверяет подпись с нашим секретом.
	// Разрешён только HS256; поле exp обязательно, истёкший токен не принимается.
	// Дополнительно проверяем издателя и допустимость времени выдачи iat.
	// Просто прочитать JSON из токена недостаточно: подпись обязательно нужно проверить.
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuer(issuer), jwt.WithIssuedAt())
	// Проверяем сведения пользователя: ID не нулевой, jti указан, sub совпадает с UserID.
	// Роль должна быть одной из двух известных. Если что-то не так, отказываем во входе по JWT.
	if err != nil || !token.Valid || claims.UserID == 0 || claims.ID == "" ||
		claims.Subject != strconv.FormatUint(uint64(claims.UserID), 10) || (claims.Role != Creator && claims.Role != Moderator) {
		return nil, ErrInvalidToken
	}
	// Правильная подпись ещё не означает, что токен можно использовать.
	// Обращаемся к Redis: возможно, пользователь уже вышел с этим JWT.
	revoked, err := s.blacklist.Exists(ctx, raw)
	if err != nil {
		return nil, ErrBlacklistUnavailable
	}
	// Найденный ключ означает, что токен отозван. Защищённый запрос будет отклонён.
	if revoked {
		return nil, ErrRevokedToken
	}
	return claims, nil
}

// Реальный выход: сервер отзывает текущий JWT, добавляя его в blacklist.
// Другие токены этого же пользователя автоматически не отзываются.
func (s *Service) Logout(ctx context.Context, raw string, claims *Claims) error {
	// Keep the blacklist entry only until this token's actual expiration.
	// time.Until вычисляет оставшееся время до exp этого конкретного JWT.
	// После его истечения запись не нужна: токен и так станет недействительным.
	if err := s.blacklist.Revoke(ctx, raw, time.Until(claims.ExpiresAt.Time)); err != nil {
		return ErrBlacklistUnavailable
	}
	return nil
}

// Отправляем JSON с ошибкой и останавливаем дальнейшую обработку запроса.
// WWW-Authenticate: Bearer сообщает клиенту, какой вид входа требуется.
func fail(c *gin.Context, code int, message string) {
	if code == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", "Bearer")
	}
	c.AbortWithStatusJSON(code, gin.H{"status": "fail", "message": message})
}

// Middleware requires a Bearer JWT for private methods. Public reads use a
// valid JWT for personalization and otherwise continue as an anonymous visitor.
// Middleware — проверка перед основным обработчиком маршрута.
// required=true: нужен действующий JWT. required=false: чтение разрешено и гостю.
// roles — перечень ролей, которым можно вызывать этот метод.
func (s *Service) Middleware(required bool, roles ...Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Получаем заголовок Authorization и делим его на слово Bearer и сам JWT.
		// Пароль и cookie здесь не читаются.
		parts := strings.Fields(c.GetHeader("Authorization"))
		// Заголовок должен содержать ровно два элемента. Bearer проверяется без учёта регистра.
		// На защищённом маршруте без такого заголовка возвращаем 401.
		// На публичном маршруте продолжаем запрос как гостевой через c.Next().
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			if required {
				fail(c, 401, "Необходим Authorization: Bearer <JWT>")
			} else {
				c.Next()
			}
			return
		}
		// Берём вторую часть заголовка — непосредственно токен.
		raw := parts[1]
		// Проверяем подпись, срок, сведения пользователя и blacklist.
		claims, err := s.Validate(c.Request.Context(), raw)
		if err != nil {
			// Публичные список и лента работают даже с неверным, истёкшим или отозванным JWT.
			// Если токен не удалось проверить, данные выдаём как гостю, без его персональных признаков.
			if !required {
				c.Next()
				return
			}
			// На защищённом маршруте сбой Redis — 503: права временно нельзя проверить.
			// Недействительный или отозванный JWT — 401.
			if errors.Is(err, ErrBlacklistUnavailable) {
				fail(c, 503, "Redis недоступен; проверка авторизации временно невозможна")
			} else {
				fail(c, 401, err.Error())
			}
			return
		}
		// Здесь начинается проверка разрешений: есть ли роль из JWT в списке разрешённых ролей?
		// Это часть авторизации. Даже действующий JWT не даёт право на любой маршрут.
		if len(roles) > 0 {
			allowed := false
			for _, role := range roles {
				if role == claims.Role {
					allowed = true
					break
				}
			}
			// Пользователь известен, но его роль не допущена: возвращаем 403.
			if !allowed {
				fail(c, 403, "Роль не имеет доступа к этому методу")
				return
			}
		}
		// Сохраняем проверенный ID и роль в контексте только этого запроса.
		// Обработчики читают их через currentuser.Get(c); чужой одновременный запрос имеет свой контекст.
		c.Set("identity", currentuser.Identity{UserID: claims.UserID, Role: string(claims.Role)})
		// Строка JWT и его claims понадобятся при выходе для записи в blacklist.
		c.Set("jwt", raw)
		c.Set("jwtClaims", claims)
		// Все проверки пройдены: передаём запрос основному обработчику.
		// Право изменить конкретный тариф затем дополнительно проверяется по его владельцу.
		c.Next()
	}
}
