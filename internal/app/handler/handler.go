package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

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

type Handler struct {
	Repository *repository.Repository
}

// Во второй лабораторной авторизации ещё нет, поэтому пользователь временно
// фиксирован так же, как в примере методических указаний.
const currentUserID uint = 1

func NewHandler(r *repository.Repository) *Handler {
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
	tariff, err := h.Repository.GetDraftTariff(currentUserID)
	if err != nil {
		logrus.Error(err)
		ctx.String(http.StatusInternalServerError, "Не удалось получить черновик")
		return
	}

	h.applyMediaFallbacks(tariff)

	ctx.HTML(http.StatusOK, "tariff_draft.html", gin.H{
		"tariff":    tariff,
		"hasDraft":  tariff != nil,
		"activeTab": "draft",
	})
}

// CreateTariffDraft — первый POST через ORM: название и URL медиа сохраняются
// только при отправке формы кнопкой «Далее».
func (h *Handler) CreateTariffDraft(ctx *gin.Context) {
	tariffName := strings.TrimSpace(ctx.PostForm("tariff_name"))
	imageURL := strings.TrimSpace(ctx.PostForm("image_url"))
	videoURL := strings.TrimSpace(ctx.PostForm("video_url"))
	if tariffName == "" || imageURL == "" || videoURL == "" {
		ctx.String(http.StatusBadRequest, "Название, URL фото и URL видео обязательны")
		return
	}

	err := h.Repository.CreateDraftTariff(&ds.CloudTariff{
		TariffName:   tariffName,
		ImageURL:     imageURL,
		VideoURL:     videoURL,
		TariffStatus: ds.StatusDraft,
		CreatorID:    currentUserID,
	})
	if err != nil && !errors.Is(err, repository.ErrDraftExists) {
		logrus.Error(err)
		ctx.String(http.StatusInternalServerError, "Не удалось создать черновик")
		return
	}

	ctx.Redirect(http.StatusSeeOther, "/tariffs/draft")
}

// PublishTariffDraft — второй POST через ORM: дополняет черновик и публикует его.
func (h *Handler) PublishTariffDraft(ctx *gin.Context) {
	tariffID, err := requiredPositiveUint(ctx.PostForm("tariff_id"))
	if err != nil {
		ctx.String(http.StatusBadRequest, "Некорректный ID тарифа")
		return
	}
	price, err := requiredNonNegativeInt(ctx.PostForm("price_per_month"))
	if err != nil {
		ctx.String(http.StatusBadRequest, "Цена должна быть целым неотрицательным числом")
		return
	}
	ram, err := requiredPositiveInt(ctx.PostForm("ram_gb"))
	if err != nil {
		ctx.String(http.StatusBadRequest, "RAM должна быть положительным целым числом")
		return
	}

	input := repository.PublishTariffInput{
		TariffID:         tariffID,
		CreatorID:        currentUserID,
		TariffName:       strings.TrimSpace(ctx.PostForm("tariff_name")),
		ImageURL:         strings.TrimSpace(ctx.PostForm("image_url")),
		VideoURL:         strings.TrimSpace(ctx.PostForm("video_url")),
		ShortDescription: strings.TrimSpace(ctx.PostForm("short_description")),
		PricePerMonth:    price,
		RAMGB:            ram,
	}
	if input.TariffName == "" || input.ImageURL == "" || input.VideoURL == "" || input.ShortDescription == "" {
		ctx.String(http.StatusBadRequest, "Все поля тарифа обязательны")
		return
	}

	if err := h.Repository.PublishDraftTariff(input); err != nil {
		logrus.Error(err)
		status := http.StatusInternalServerError
		if errors.Is(err, repository.ErrTariffNotFound) {
			status = http.StatusNotFound
		}
		ctx.String(status, "Не удалось опубликовать черновик")
		return
	}

	ctx.Redirect(http.StatusSeeOther, "/tariffs/feed?id="+strconv.FormatUint(uint64(tariffID), 10))
}

// DeleteTariff — третий POST; репозиторий выполняет сырой SQL UPDATE без ORM.
func (h *Handler) DeleteTariff(ctx *gin.Context) {
	tariffID, err := requiredPositiveUint(ctx.PostForm("tariff_id"))
	if err != nil {
		ctx.String(http.StatusBadRequest, "Некорректный ID тарифа")
		return
	}

	if err := h.Repository.DeleteTariff(ctx.Request.Context(), tariffID); err != nil {
		logrus.Error(err)
		status := http.StatusInternalServerError
		if errors.Is(err, repository.ErrTariffNotFound) {
			status = http.StatusNotFound
		}
		ctx.String(status, "Не удалось удалить тариф")
		return
	}

	ctx.Redirect(http.StatusSeeOther, "/tariffs")
}

func requiredPositiveUint(value string) (uint, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, errors.New("ожидалось положительное число")
	}
	return uint(parsed), nil
}

func requiredPositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("ожидалось положительное число")
	}
	return parsed, nil
}

func requiredNonNegativeInt(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, errors.New("ожидалось неотрицательное число")
	}
	return parsed, nil
}
