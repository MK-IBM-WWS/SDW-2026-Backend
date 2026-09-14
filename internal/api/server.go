package api

import (
	"fmt"
	"log"

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
	router := gin.Default()
	// Подключаем исходную нижнюю панель и три страницы явно, чтобы общий
	// шаблон tabbar.html гарантированно загружался вместе с каждой страницей.
	router.LoadHTMLFiles(
		"templates/tabbar.html",
		"templates/tariff_tiles.html",
		"templates/tariff_feed.html",
		"templates/tariff_draft.html",
	)
	router.Static("/static", "./resources")

	// Ровно шесть HTTP-маршрутов по заданию: 3 GET и 3 POST.
	router.GET("/tariffs", tariffHandler.GetTariffTiles)
	router.GET("/tariffs/feed", tariffHandler.GetTariffFeed)
	router.GET("/tariffs/draft", tariffHandler.GetTariffDraft)
	router.POST("/tariffs/draft", tariffHandler.CreateTariffDraft)
	router.POST("/tariffs/publish", tariffHandler.PublishTariffDraft)
	router.POST("/tariffs/delete", tariffHandler.DeleteTariff)

	address := fmt.Sprintf("%s:%d", serviceConfig.ServiceHost, serviceConfig.ServicePort)
	return router.Run(address)
}
