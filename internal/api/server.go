package api

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"cloud-tariffs-backend/internal/app/handler"
	"cloud-tariffs-backend/internal/app/repository"
)

func StartServer() error {
	log.Println("Server start up")

	repo, err := repository.NewRepository()
	if err != nil {
		return fmt.Errorf("initialize repository: %w", err)
	}

	tariffHandler := handler.NewHandler(repo)
	router := gin.Default()
	router.LoadHTMLGlob("templates/*")
	router.Static("/static", "./resources")
	router.GET("/tariffs", tariffHandler.GetTariffTiles)
	router.GET("/tariffs/feed", tariffHandler.GetTariffFeed)
	router.GET("/tariffs/feed/:id", tariffHandler.GetTariffFeed)
	router.GET("/tariffs/draft", tariffHandler.GetTariffDraft)

	address := os.Getenv("SERVER_ADDRESS")
	if address == "" {
		address = ":8080"
	}

	return router.Run(address)
}
