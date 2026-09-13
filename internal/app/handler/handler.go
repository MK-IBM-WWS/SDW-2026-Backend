package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"cloud-tariffs-backend/internal/app/repository"
)

var allowedPriceLimits = map[string]int{
	"1000":   1000,
	"10000":  10000,
	"500000": 500000,
}

type Handler struct {
	Repository *repository.Repository
}

func NewHandler(r *repository.Repository) *Handler {
	return &Handler{Repository: r}
}

func (h *Handler) GetTariffTiles(ctx *gin.Context) {
	priceLimit := ctx.Query("priceLimit")

	var (
		tariffs []repository.CloudTariff
		err     error
	)

	if priceLimit == "" {
		tariffs, err = h.Repository.GetPublishedTariffs()
	} else if limit, exists := allowedPriceLimits[priceLimit]; exists {
		tariffs, err = h.Repository.GetTariffsByMaxPrice(limit)
	} else {
		ctx.String(http.StatusBadRequest, "Допустимые значения priceLimit: 1000, 10000 или 500000")
		return
	}

	if err != nil {
		logrus.Error(err)
		ctx.String(http.StatusInternalServerError, "Не удалось получить список тарифов")
		return
	}

	ctx.HTML(http.StatusOK, "tariff_tiles.html", gin.H{
		"tariffs":    tariffs,
		"priceLimit": priceLimit,
		"activeTab":  "tiles",
	})
}

func (h *Handler) GetTariffFeed(ctx *gin.Context) {
	var (
		tariff repository.CloudTariff
		err    error
	)

	idText := ctx.Param("id")
	if idText == "" {
		tariff, err = h.Repository.GetFirstTariff()
	} else {
		tariffID, convErr := strconv.Atoi(idText)
		if convErr != nil || tariffID < 1 {
			ctx.String(http.StatusBadRequest, "Некорректный идентификатор тарифа")
			return
		}

		if ctx.Query("next") == "true" {
			tariff, err = h.Repository.GetNextTariff(tariffID)
		} else {
			tariff, err = h.Repository.GetPublishedTariff(tariffID)
		}
	}

	if err != nil {
		logrus.Error(err)
		ctx.String(http.StatusNotFound, "Тариф не найден или недоступен")
		return
	}

	ctx.HTML(http.StatusOK, "tariff_feed.html", gin.H{
		"tariff":    tariff,
		"activeTab": "feed",
	})
}

func (h *Handler) GetTariffDraft(ctx *gin.Context) {
	tariff, err := h.Repository.GetDraftTariff()
	if err != nil {
		logrus.Error(err)
		ctx.String(http.StatusNotFound, "Черновик тарифа не найден")
		return
	}

	ctx.HTML(http.StatusOK, "tariff_draft.html", gin.H{
		"tariff":    tariff,
		"activeTab": "draft",
	})
}

