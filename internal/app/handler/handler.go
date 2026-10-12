package handler

import (
	"cloud-tariffs-backend/internal/app/auth"
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"context"
)

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

type Handler struct {
	Repository Repository
	Auth       *auth.Service
}

func NewHandler(r Repository) *Handler {
	return &Handler{Repository: r}
}
