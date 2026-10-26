package handler

import (
	"cloud-tariffs-backend/internal/app/auth"
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"context"
)

// Обработчики знают этот набор операций, а не детали PostgreSQL и MinIO.
// В ЛР4 добавлены поиск пользователя по логину и получение прямых ссылок на медиа.
type Repository interface {
	MediaURL(context.Context, string) (string, error)
	GetPublishedTariffs(*int, uint) ([]ds.CloudTariff, error)
	GetPublishedTariff(uint, uint) (*ds.CloudTariff, error)
	GetFirstPublishedTariff(uint) (*ds.CloudTariff, error)
	GetNextPublishedTariff(uint, uint) (*ds.CloudTariff, error)
	GetDraftTariff(uint) (*ds.CloudTariff, error)
	CreateWithFiles(context.Context, *ds.CloudTariff, *repository.Upload, *repository.Upload) error
	PublishDraftTariff(repository.PublishTariffInput) error
	DeleteTariff(context.Context, uint, uint) error
	SetLike(context.Context, uint, uint, int) (int64, error)
	Register(*ds.User) error
	GetUser(uint) (ds.User, error)
	GetUserByLogin(string) (ds.User, error)
}

// Handler хранит помощников для данных и JWT. Через них HTTP-методы выполняют свою работу.
type Handler struct {
	Repository Repository
	Auth       *auth.Service
}

// Создаём набор обработчиков с заданным репозиторием. JWT-сервис подключается при старте сервера.
func NewHandler(r Repository) *Handler {
	return &Handler{Repository: r}
}
