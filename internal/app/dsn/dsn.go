package dsn

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

func FromEnv() (string, error) {
	if err := godotenv.Load(); err != nil {
		return "", fmt.Errorf("не удалось загрузить файл .env: %w", err)
	}

	requiredVariables := []string{
		"DB_HOST",
		"DB_PORT",
		"DB_USER",
		"DB_PASS",
		"DB_NAME",
	}

	var missingVariables []string

	for _, variableName := range requiredVariables {
		if strings.TrimSpace(os.Getenv(variableName)) == "" {
			missingVariables = append(missingVariables, variableName)
		}
	}

	if len(missingVariables) > 0 {
		return "", fmt.Errorf(
			"в .env не заданы переменные: %s",
			strings.Join(missingVariables, ", "),
		)
	}

	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASS"),
		os.Getenv("DB_NAME"),
	), nil
}
