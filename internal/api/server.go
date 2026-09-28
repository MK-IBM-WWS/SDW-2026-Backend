package api

import (
	"fmt"
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

	tariffHandler := handler.NewHandler(repo)
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
				c.Header("Access-Control-Allow-Headers", "Content-Type")
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
	router.LoadHTMLFiles(
		"templates/tabbar.html",
		"templates/tariff_tiles.html",
		"templates/tariff_feed.html",
		"templates/tariff_draft.html",
	)
	router.Static("/static", "./resources")

	router.GET("/tariffs", tariffHandler.GetTariffTiles)
	router.GET("/tariffs/feed", tariffHandler.GetTariffFeed)
	router.GET("/tariffs/draft", tariffHandler.GetTariffDraft)
	api := router.Group("/api")
	api.GET("/tariffs", tariffHandler.ListAPI)
	api.GET("/tariffs/feed", tariffHandler.FeedAPI)
	api.GET("/tariffs/draft", tariffHandler.DraftAPI)
	api.GET("/tariffs/:id", tariffHandler.DetailAPI)
	api.GET("/tariffs/:id/image", tariffHandler.MediaAPI)
	api.GET("/tariffs/:id/video", tariffHandler.MediaAPI)
	api.POST("/tariffs", tariffHandler.CreateAPI)
	api.PUT("/tariffs/:id/publication", tariffHandler.PublishAPI)
	api.DELETE("/tariffs/:id", tariffHandler.DeleteAPI)
	api.POST("/tariffs/:id/likes", tariffHandler.LikeAPI)
	api.POST("/users", tariffHandler.RegisterAPI)
	api.POST("/users/login", tariffHandler.LoginAPI)
	api.POST("/users/logout", tariffHandler.LogoutAPI)
	api.GET("/users/:id/tariffs", tariffHandler.UserTariffsAPI)
	router.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/tariffs") })

	address := fmt.Sprintf("%s:%d", serviceConfig.ServiceHost, serviceConfig.ServicePort)
	server := &http.Server{Addr: address, Handler: router, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second}
	return server.ListenAndServe()
}
