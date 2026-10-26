package handler

import (
	"errors"
	"strings"

	"cloud-tariffs-backend/internal/app/auth"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ЛР4. Данные для регистрации и входа: только login и password.
// Теги json задают имена полей в запросе, example показывает пример в Swagger.
type Credentials struct {
	Login    string `json:"login" example:"student"`
	Password string `json:"password" example:"student123"`
}

// Ответ после успешного входа. AccessToken — JWT, TokenType — Bearer.
// ExpiresIn — срок в секундах; UserID и Role помогают клиенту понять, под кем он вошёл.
type TokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type" example:"Bearer"`
	ExpiresIn   int64     `json:"expires_in" example:"3600"`
	UserID      uint      `json:"user_id" example:"1"`
	Role        auth.Role `json:"role" example:"creator"`
}

// Аутентификация: сравниваем логин и пароль с настоящей записью PostgreSQL.
// Строки с @ ниже описывают этот HTTP-метод для генератора Swagger.
// Они документируют запрос, но не выполняют проверку пользователя.
// LoginAPI godoc
// @Summary Войти и получить JWT
// @Tags Пользователи
// @Produce json
// @Accept json
// @Param body body Credentials true "Логин и пароль"
// @Success 200 {object} TokenEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /users/login [post]
// @Failure 401 {object} ErrorEnvelope "Неверный логин или пароль"
func (h *Handler) LoginAPI(c *gin.Context) {
	// У входа нет параметров после ? в адресе: данные передаются в JSON-теле.
	if !noQuery(c) {
		return
	}
	// Создаём место для прочитанного логина и пароля. &body — адрес этой структуры.
	var body Credentials
	// Читаем JSON и отказываем при неизвестных полях или неправильном формате.
	// Клиент не сможет добавить сюда user_id или role и выбрать чужой аккаунт.
	if !strictJSON(c, &body) {
		return
	}
	// Убираем пробелы по краям и приводим логин к нижнему регистру.
	// Сам пароль не изменяем: пробел и регистр могут быть его частью.
	body.Login = strings.ToLower(strings.TrimSpace(body.Login))
	if body.Login == "" || len(body.Login) > 50 || body.Password == "" || len(body.Password) > 72 {
		Fail(c, 400, "Нужны login и password (пароль не более 72 байт)")
		return
	}
	// Ищем пользователя по логину в PostgreSQL. Это ещё не подтверждение его личности.
	user, err := h.Repository.GetUserByLogin(body.Login)
	if err != nil {
		// При неизвестном логине отвечаем так же, как при неправильном пароле.
		// Не раскрываем через сообщение об ошибке, есть ли такой аккаунт.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, 401, "Неверный логин или пароль")
		} else {
			apiError(c, err)
		}
		return
	}
	// bcrypt проверяет, подходит ли введённый пароль к сохранённому хешу.
	// Пароль не расшифровываем: исходного пароля в БД нет.
	// Если проверка не пройдена, выдачу JWT не выполняем.
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		Fail(c, 401, "Неверный логин или пароль")
		return
	}
	// Логин и пароль подтверждены. Выпускаем JWT с ID и ролью из записи пользователя.
	token, claims, err := h.Auth.Issue(user)
	if err != nil {
		apiError(c, err)
		return
	}
	// Просим клиент и промежуточные кэши не сохранять ответ с токеном.
	c.Header("Cache-Control", "no-store")
	// Возвращаем JWT и сведения о входе. Seconds() переводит Go duration в секунды.
	// Cookie здесь не устанавливается: клиент дальше передаёт Authorization: Bearer <JWT>.
	success(c, 200, TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(h.Auth.TTL().Seconds()), UserID: user.UserID, Role: claims.Role})
}

// Деавторизация в нашей работе — выход и отзыв текущего токена.
// Перед этим методом middleware уже проверил JWT и положил его в контекст запроса.
// LogoutAPI godoc
// @Summary Выйти и отозвать текущий JWT
// @Tags Пользователи
// @Produce json
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 200 {object} LogoutEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /users/logout [post]
func (h *Handler) LogoutAPI(c *gin.Context) {
	// Выходу не нужны ни параметры URL, ни JSON с ID: токен уже есть в заголовке.
	if !noQuery(c) || !noBody(c) {
		return
	}
	// Достаём проверенные claims, сохранённые middleware.
	// Преобразование value.(*auth.Claims) уточняет тип; ok показывает, что оно удалось.
	value, exists := c.Get("jwtClaims")
	claims, ok := value.(*auth.Claims)
	// Если проверенных данных пользователя почему-то нет, безопасно отказываем с 401.
	if !exists || !ok {
		Fail(c, 401, "Необходима авторизация")
		return
	}
	// Берём исходный JWT из контекста и передаём его сервису для отзыва в Redis.
	err := h.Auth.Logout(c.Request.Context(), c.GetString("jwt"), claims)
	if err != nil {
		// Если записать blacklist не удалось, нельзя сообщать, что выход выполнен.
		Fail(c, 503, "Не удалось отозвать токен: Redis недоступен")
		return
	}
	// Запись blacklist сохранена: подтверждаем выход.
	// Клиент должен убрать JWT у себя; сервер уже отклонит повторный защищённый запрос с ним.
	success(c, 200, gin.H{"logged_out": true})
}
