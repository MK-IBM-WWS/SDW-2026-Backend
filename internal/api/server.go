package api

import (
	"cloud-tariffs-backend/docs"
	"cloud-tariffs-backend/internal/app/auth"
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"cloud-tariffs-backend/internal/app/config"
	"cloud-tariffs-backend/internal/app/dsn"
	"cloud-tariffs-backend/internal/app/handler"
	"cloud-tariffs-backend/internal/app/repository"
)

// ЛР4. При запуске собираем приложение: настройки, PostgreSQL/MinIO, Redis, JWT и HTTP-маршруты.
func StartServer() error {
	log.Println("Server start up")

	// Читаем адрес и порт сервера из config/config.toml.
	serviceConfig, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("чтение config/config.toml: %w", err)
	}

	// Собираем параметры подключения к PostgreSQL из переменных окружения.
	// В этом проекте dsn.FromEnv также загружает значения .env.
	databaseDSN, err := dsn.FromEnv()
	if err != nil {
		return fmt.Errorf("чтение параметров PostgreSQL: %w", err)
	}

	// Готовим доступ к данным и файлам. Репозиторий знает, как обращаться к PostgreSQL и MinIO.
	repo, err := repository.New(databaseDSN)

	if err != nil {
		return fmt.Errorf("initialize repository: %w", err)
	}
	// defer выполнит закрытие подключения, когда функция закончит работу.
	defer repo.Close()

	// Преобразуем JWT_EXPIRES_IN, например 1h, в длительность Go.
	ttl, err := time.ParseDuration(os.Getenv("JWT_EXPIRES_IN"))
	if err != nil {
		return fmt.Errorf("JWT_EXPIRES_IN: %w", err)
	}
	// Готовим клиент Redis с адресом, паролем и ограниченным ожиданием соединения.
	// Настройки таймаутов не дают каждому запросу бесконечно ждать недоступное хранилище.
	redisClient := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxRetries: 1})
	defer redisClient.Close()
	// Для проверки Redis отводим максимум пять секунд. cancel освобождает связанные ресурсы.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Команда PING проверяет, что Redis отвечает. Без него защищённые операции не готовы к работе.
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis PING: %w", err)
	}
	// Передаём JWT-сервису секрет, срок жизни токена и Redis blacklist.
	authService, err := auth.New(os.Getenv("JWT_SECRET"), ttl, auth.RedisBlacklist{Client: redisClient})
	if err != nil {
		return err
	}
	tariffHandler := handler.NewHandler(repo)
	// Обработчики входа и выхода будут пользоваться этим же JWT-сервисом.
	tariffHandler.Auth = authService
	router := NewRouter(tariffHandler, authService)

	address := fmt.Sprintf("%s:%d", serviceConfig.ServiceHost, serviceConfig.ServicePort)
	// Создаём HTTP-сервер. Таймауты ограничивают ожидание заголовков, тела и отправки ответа.
	server := &http.Server{Addr: address, Handler: router, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second}
	return server.ListenAndServe()
}

// RegisterAPI centralizes route authorization so it is exercised by HTTP tests.
// Здесь назначаем обработчики и правила доступа для ровно десяти API-методов.
func RegisterAPI(router *gin.Engine, h *handler.Handler, a *auth.Service) {
	// Для списка и ленты JWT необязателен: он нужен только для is_owner и is_liked.
	// Middleware(false) при плохом JWT продолжит запрос от имени гостя.
	public := router.Group("/api", a.Middleware(false))
	public.GET("/tariffs", h.ListAPI)
	public.GET("/tariffs/feed", h.FeedAPI)
	// Login/register remain accessible even when a previously issued JWT expires.
	// Регистрация и вход доступны без старого JWT, иначе пользователь не смог бы войти впервые.
	router.POST("/api/users", h.RegisterAPI)
	router.POST("/api/users/login", h.LoginAPI)
	// Middleware(true, ...) требует JWT и допустимую роль до вызова обработчика.
	// Так защищаем черновик, создание, публикацию, удаление, лайк и выход.
	private := router.Group("/api", a.Middleware(true, auth.Creator, auth.Moderator))
	private.GET("/tariffs/draft", h.DraftAPI)
	private.POST("/tariffs", h.CreateAPI)
	private.PUT("/tariffs/:id/publication", h.PublishAPI)
	private.DELETE("/tariffs/:id", h.DeleteAPI)
	private.POST("/tariffs/:id/likes", h.LikeAPI)
	private.POST("/users/logout", h.LogoutAPI)
}

// NewRouter serves only Swagger, JSON API and fallback media.
// Создаём маршрутизатор: он выбирает обработчик по HTTP-методу и пути запроса.
func NewRouter(h *handler.Handler, a *auth.Service) *gin.Engine {
	router := gin.New()
	// Logger записывает запросы в журнал; Recovery ловит аварийную ошибку обработчика и возвращает 500.
	router.Use(gin.Logger(), gin.CustomRecovery(func(c *gin.Context, recovered any) {
		handler.Fail(c, 500, "Внутренняя ошибка сервера")
	}))
	router.HandleMethodNotAllowed = true
	router.Use(func(c *gin.Context) {
		// CORS касается браузера: разрешаем обращения с адресов из CORS_ORIGINS.
		// Это не вход и не проверка прав. Postman не подчиняется браузерным правилам CORS.
		origin := c.GetHeader("Origin")
		for _, allowed := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
			if origin != "" && origin == strings.TrimSpace(allowed) {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
				// OPTIONS — предварительный вопрос браузера, разрешён ли будущий запрос с такими заголовками.
				if c.Request.Method == "OPTIONS" {
					c.AbortWithStatus(204)
					return
				}
			}
		}
		c.Next()
	})
	// Для неизвестного пути возвращаем 404; для неподдерживаемого HTTP-метода — 405.
	router.NoRoute(func(c *gin.Context) { handler.Fail(c, 404, "Маршрут не найден") })
	router.NoMethod(func(c *gin.Context) { handler.Fail(c, 405, "Метод не поддерживается") })
	// Оставляем локальные изображения и видео для заглушек, даже когда фронтенд удалён.
	router.Static("/static/media", "./resources/media")
	// Swagger uses the browser's current origin instead of a hard-coded host.
	// Swagger обращается к тому же адресу, по которому открыта его страница.
	docs.SwaggerInfo.Host = ""
	// Отдаём Swagger UI и сгенерированное описание API.
	// UI отправляет настоящие запросы, а правила доступа всё равно проверяет middleware.
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	RegisterAPI(router, h, a)
	// При открытии корня сайта переводим браузер на Swagger: обычного фронтенда в ЛР4 нет.
	router.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/swagger/index.html") })

	return router
}
