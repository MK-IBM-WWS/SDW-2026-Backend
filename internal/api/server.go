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

func StartServer() error {
	log.Println("Server start up")

	serviceConfig, err := config.NewConfig()
	if err != nil {
		return fmt.Errorf("чтение config/config.toml: %w", err)
	}

	databaseDSN, err := dsn.FromEnv()
	if err != nil {
		return fmt.Errorf("чтение параметров PostgreSQL: %w", err)
	}

	repo, err := repository.New(databaseDSN)

	if err != nil {
		return fmt.Errorf("initialize repository: %w", err)
	}
	defer repo.Close()

	ttl, err := time.ParseDuration(os.Getenv("JWT_EXPIRES_IN"))
	if err != nil {
		return fmt.Errorf("JWT_EXPIRES_IN: %w", err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR"), Password: os.Getenv("REDIS_PASSWORD"), DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxRetries: 1})
	defer redisClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis PING: %w", err)
	}
	authService, err := auth.New(os.Getenv("JWT_SECRET"), ttl, auth.RedisBlacklist{Client: redisClient})
	if err != nil {
		return err
	}
	tariffHandler := handler.NewHandler(repo)
	tariffHandler.Auth = authService
	router := NewRouter(tariffHandler, authService)

	address := fmt.Sprintf("%s:%d", serviceConfig.ServiceHost, serviceConfig.ServicePort)
	server := &http.Server{Addr: address, Handler: router, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second}
	return server.ListenAndServe()
}

// RegisterAPI centralizes route authorization so it is exercised by HTTP tests.
func RegisterAPI(router *gin.Engine, h *handler.Handler, a *auth.Service) {
	public := router.Group("/api", a.Middleware(false))
	public.GET("/tariffs", h.ListAPI)
	public.GET("/tariffs/feed", h.FeedAPI)
	// Login/register remain accessible even when a previously issued JWT expires.
	router.POST("/api/users", h.RegisterAPI)
	router.POST("/api/users/login", h.LoginAPI)
	private := router.Group("/api", a.Middleware(true, auth.Creator, auth.Moderator))
	private.GET("/tariffs/draft", h.DraftAPI)
	private.POST("/tariffs", h.CreateAPI)
	private.PUT("/tariffs/:id/publication", h.PublishAPI)
	private.DELETE("/tariffs/:id", h.DeleteAPI)
	private.POST("/tariffs/:id/likes", h.LikeAPI)
	private.POST("/users/logout", h.LogoutAPI)
}

// NewRouter serves only Swagger, JSON API and fallback media.
func NewRouter(h *handler.Handler, a *auth.Service) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.CustomRecovery(func(c *gin.Context, recovered any) {
		handler.Fail(c, 500, "Внутренняя ошибка сервера")
	}))
	router.HandleMethodNotAllowed = true
	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		for _, allowed := range strings.Split(os.Getenv("CORS_ORIGINS"), ",") {
			if origin != "" && origin == strings.TrimSpace(allowed) {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if c.Request.Method == "OPTIONS" {
					c.AbortWithStatus(204)
					return
				}
			}
		}
		c.Next()
	})
	router.NoRoute(func(c *gin.Context) { handler.Fail(c, 404, "Маршрут не найден") })
	router.NoMethod(func(c *gin.Context) { handler.Fail(c, 405, "Метод не поддерживается") })
	router.Static("/static/media", "./resources/media")
	// Swagger uses the browser's current origin instead of a hard-coded host.
	docs.SwaggerInfo.Host = ""
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/swagger/doc.json")))
	RegisterAPI(router, h, a)
	router.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/swagger/index.html") })

	return router
}
