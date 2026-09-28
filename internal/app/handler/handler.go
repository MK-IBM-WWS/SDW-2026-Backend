package handler

import (
	"cloud-tariffs-backend/internal/app/currentuser"
	"context"
	"errors"
	"github.com/minio/minio-go/v7"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
)

var allowedPriceLimits = map[string]int{
	"1000":   1000,
	"10000":  10000,
	"500000": 500000,
}

// Repository makes HTTP handlers independently testable.
type Repository interface {
	GetPublishedTariffs(*int) ([]ds.CloudTariff, error)
	GetPublishedTariff(uint) (*ds.CloudTariff, error)
	GetFirstPublishedTariff() (*ds.CloudTariff, error)
	GetNextPublishedTariff(uint) (*ds.CloudTariff, error)
	GetDraftTariff(uint) (*ds.CloudTariff, error)
	CreateWithFiles(context.Context, *ds.CloudTariff, *repository.Upload, *repository.Upload) error
	PublishDraftTariff(repository.PublishTariffInput) error
	DeleteTariff(context.Context, uint) error
	SetLike(context.Context, uint, int) (int64, error)
	Register(*ds.User) error
	GetUser(uint) (ds.User, error)
	UserTariffs(uint) (*ds.User, []ds.CloudTariff, error)
	VisibleTariff(uint, uint) (*ds.CloudTariff, error)
	OpenMedia(context.Context, string) (*minio.Object, minio.ObjectInfo, error)
}

type Handler struct {
	Repository Repository
}

func NewHandler(r Repository) *Handler {
	return &Handler{Repository: r}
}

func (h *Handler) GetTariffTiles(ctx *gin.Context) {
	priceLimit := ctx.Query("priceLimit")

	var maxPrice *int

	if priceLimit != "" {
		limit, exists := allowedPriceLimits[priceLimit]
		if !exists {
			ctx.String(http.StatusBadRequest, "Допустимые значения priceLimit: 1000, 10000 или 500000")
			return
		}
		maxPrice = &limit
	}

	tariffs, err := h.Repository.GetPublishedTariffs(maxPrice)
	if err != nil {
		logrus.Error(err)
		ctx.String(http.StatusInternalServerError, "Не удалось получить список тарифов")
		return
	}

	for i := range tariffs {
		h.applyMediaFallbacks(&tariffs[i])
	}

	ctx.HTML(http.StatusOK, "tariff_tiles.html", gin.H{
		"tariffs":    tariffs,
		"priceLimit": priceLimit,
		"activeTab":  "tiles",
	})
}

func (h *Handler) GetTariffFeed(ctx *gin.Context) {
	idText := ctx.Query("id")
	if idText == "" {
		tariff, err := h.Repository.GetFirstPublishedTariff()
		h.renderFeed(ctx, tariff, err)
		return
	}

	tariffID, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || tariffID == 0 {
		ctx.String(http.StatusBadRequest, "Некорректный идентификатор тарифа")
		return
	}

	var tariff *ds.CloudTariff
	if ctx.Query("next") == "true" {
		tariff, err = h.Repository.GetNextPublishedTariff(uint(tariffID))
	} else {
		tariff, err = h.Repository.GetPublishedTariff(uint(tariffID))
	}
	h.renderFeed(ctx, tariff, err)
}

func (h *Handler) renderFeed(ctx *gin.Context, tariff *ds.CloudTariff, err error) {
	if err != nil {
		logrus.Error(err)
		status := http.StatusInternalServerError
		if errors.Is(err, repository.ErrTariffNotFound) {
			status = http.StatusNotFound
		}
		ctx.String(status, "Тариф не найден или недоступен")
		return
	}

	h.applyMediaFallbacks(tariff)

	ctx.HTML(http.StatusOK, "tariff_feed.html", gin.H{
		"tariff":    tariff,
		"activeTab": "feed",
	})
}

func (h *Handler) GetTariffDraft(ctx *gin.Context) {
	tariff, err := h.Repository.GetDraftTariff(currentuser.Get().ID())
	if err != nil {
		logrus.Error(err)
		ctx.String(http.StatusInternalServerError, "Не удалось получить черновик")
		return
	}

	// Keep the database values for the publish form; only the preview may use fallbacks.
	var preview *ds.CloudTariff
	if tariff != nil {
		copy := *tariff
		h.applyMediaFallbacks(&copy)
		preview = &copy
	}

	ctx.HTML(http.StatusOK, "tariff_draft.html", gin.H{
		"tariff":    tariff,
		"preview":   preview,
		"hasDraft":  tariff != nil,
		"activeTab": "draft",
	})
}
