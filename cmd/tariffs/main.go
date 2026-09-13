package main

import (
	"log"

	"cloud-tariffs-backend/internal/api"
)

func main() {
	log.Println("Application start")

	if err := api.StartServer(); err != nil {
		log.Fatalf("application stopped with an error: %v", err)
	}
}

