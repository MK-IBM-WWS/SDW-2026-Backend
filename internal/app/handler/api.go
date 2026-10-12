package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"cloud-tariffs-backend/internal/app/currentuser"
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func Fail(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"status": "fail", "message": msg})
}
func apiError(c *gin.Context, err error) {
	var input *repository.InputError
	switch {
	case errors.As(err, &input):
		Fail(c, 400, err.Error())
	case errors.Is(err, repository.ErrTariffNotFound) || errors.Is(err, gorm.ErrRecordNotFound):
		Fail(c, 404, "Запись не найдена")
	case errors.Is(err, repository.ErrForbidden):
		Fail(c, 403, err.Error())
	case errors.Is(err, repository.ErrDraftExists) || errors.Is(err, repository.ErrInvalidState) || errors.Is(err, repository.ErrLoginExists):
		Fail(c, 409, err.Error())
	default:
		log.Printf("request failed: %v", err)
		Fail(c, 500, "Внутренняя ошибка сервера")
	}
}
func success(c *gin.Context, code int, data any) {
	c.JSON(code, gin.H{"status": "success", "data": data})
}
func parseID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 63)
	if err != nil || id == 0 {
		Fail(c, 400, "id должен быть положительным целым числом")
		return 0, false
	}
	return uint(id), true
}
func strictJSON(c *gin.Context, dst any) bool {
	if c.ContentType() != "application/json" {
		Fail(c, 415, "Требуется Content-Type: application/json")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		Fail(c, 400, "Некорректный JSON или недопустимое поле: "+err.Error())
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		Fail(c, 400, "Ожидается один JSON-объект")
		return false
	}
	return true
}
func noQuery(c *gin.Context) bool {
	if c.Request.URL.RawQuery != "" {
		Fail(c, 400, "Этот метод не принимает query-параметры")
		return false
	}
	return true
}
func noBody(c *gin.Context) bool {
	if c.Request.Body != nil {
		b, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
		if err != nil || len(b) > 0 {
			Fail(c, 400, "Этот метод не принимает тело запроса")
			return false
		}
	}
	return true
}

type CreatorResponse struct {
	UserID uint   `json:"user_id"`
	Login  string `json:"login"`
}

type TariffResponse struct {
	ds.CloudTariff
	ImageURL string          `json:"image_url"`
	VideoURL string          `json:"video_url"`
	Creator  CreatorResponse `json:"creator"`
}

func (h *Handler) serialize(ctx context.Context, t ds.CloudTariff, viewer uint) TariffResponse {
	t.IsOwner = 0
	if viewer != 0 && t.CreatorID == viewer {
		t.IsOwner = 1
	}
	imageURL, videoURL := h.mediaURLs(ctx, t)
	return TariffResponse{CloudTariff: t, ImageURL: imageURL, VideoURL: videoURL, Creator: CreatorResponse{UserID: t.Creator.UserID, Login: t.Creator.Login}}
}

// ListAPI godoc
// @Summary Список опубликованных тарифов
// @Tags Тарифы
// @Produce json
// @Description Чтение доступно независимо от JWT. Валидный Authorization добавляет персональные is_owner/is_liked; неверный, истёкший или отозванный JWT трактуется как гостевой запрос.
// @Param Authorization header string false "Bearer <JWT>, необязательно"
// @Param priceLimit query int false "Максимальная цена в рублях, целое >= 0" minimum(0)
// @Success 200 {object} ListEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs [get]
func (h *Handler) ListAPI(c *gin.Context) {
	var max *int
	for k, v := range c.Request.URL.Query() {
		if k != "priceLimit" || len(v) != 1 {
			Fail(c, 400, "Допустим только один priceLimit")
			return
		}
	}
	if value, ok := c.GetQuery("priceLimit"); ok {
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil || n < 0 {
			Fail(c, 400, "priceLimit должен быть целым числом >= 0")
			return
		}
		v := int(n)
		max = &v
	} else if _, ok := c.Request.URL.Query()["priceLimit"]; ok {
		Fail(c, 400, "priceLimit не может быть пустым")
		return
	}
	list, err := h.Repository.GetPublishedTariffs(max, currentuser.Get(c).ID())
	if err != nil {
		apiError(c, err)
		return
	}
	result := make([]TariffResponse, 0, len(list))
	for _, t := range list {
		result = append(result, h.serialize(c.Request.Context(), t, currentuser.Get(c).ID()))
	}
	success(c, 200, result)
}

// FeedAPI godoc
// @Summary Карточка ленты
// @Tags Тарифы
// @Produce json
// @Description Чтение доступно независимо от JWT. Валидный Authorization добавляет персональные is_owner/is_liked; неверный, истёкший или отозванный JWT трактуется как гостевой запрос.
// @Param Authorization header string false "Bearer <JWT>, необязательно"
// @Param id query int false "ID опубликованной карточки"
// @Param next query boolean false "Следующая карточка; требует id"
// @Param after_id query int false "Следующая после ID; отдельно от id и next"
// @Success 200 {object} TariffEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs/feed [get]
func (h *Handler) FeedAPI(c *gin.Context) {
	query := c.Request.URL.Query()
	for key, values := range query {
		if (key != "id" && key != "next" && key != "after_id") || len(values) != 1 {
			Fail(c, 400, "Допустимы однократные параметры id, next или after_id")
			return
		}
	}
	_, hasID := query["id"]
	_, hasNext := query["next"]
	_, hasAfter := query["after_id"]
	if hasAfter && (hasID || hasNext) {
		Fail(c, 400, "after_id нельзя совмещать с id или next")
		return
	}
	if hasNext && !hasID {
		Fail(c, 400, "Для next необходимо указать id")
		return
	}

	var id uint64
	var err error
	next := false
	if hasID {
		id, err = strconv.ParseUint(query.Get("id"), 10, 63)
		if err != nil || id == 0 {
			Fail(c, 400, "id должен быть положительным целым числом")
			return
		}
		if hasNext {
			switch query.Get("next") {
			case "true":
				next = true
			case "false":
				next = false
			default:
				Fail(c, 400, "next должен быть true или false")
				return
			}
		}
	}
	if hasAfter {
		id, err = strconv.ParseUint(query.Get("after_id"), 10, 63)
		if err != nil {
			Fail(c, 400, "Некорректный after_id")
			return
		}
	}

	var tariff *ds.CloudTariff
	exactCard := hasID && !next
	if exactCard {
		tariff, err = h.Repository.GetPublishedTariff(uint(id), currentuser.Get(c).ID())
	} else {
		tariff, err = h.Repository.GetNextPublishedTariff(uint(id), currentuser.Get(c).ID())
	}
	if errors.Is(err, repository.ErrTariffNotFound) && !exactCard {
		success(c, 200, nil)
		return
	}
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, h.serialize(c.Request.Context(), *tariff, currentuser.Get(c).ID()))
}

// DraftAPI godoc
// @Summary Единственный черновик текущего пользователя
// @Tags Тарифы
// @Produce json
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 200 {object} TariffEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs/draft [get]
func (h *Handler) DraftAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	t, err := h.Repository.GetDraftTariff(currentuser.Get(c).ID())
	if err != nil {
		apiError(c, err)
		return
	}
	if t == nil {
		success(c, 200, nil)
		return
	}
	success(c, 200, h.serialize(c.Request.Context(), *t, currentuser.Get(c).ID()))
}

// CreateAPI godoc
// @Summary Создать черновик с фото и видео
// @Tags Тарифы
// @Produce json
// @Accept multipart/form-data
// @Param tariff_name formData string true "Название, 1–100 символов"
// @Param image formData file false "Фото JPEG/PNG/GIF/WebP, до 10 MiB"
// @Param video formData file false "Видео MP4/WebM, до 50 MiB"
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 201 {object} TariffEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs [post]
func (h *Handler) CreateAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	if c.ContentType() != "multipart/form-data" {
		Fail(c, 415, "Требуется multipart/form-data с файлами")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 62<<20)
	err := c.Request.ParseMultipartForm(2 << 20)
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			Fail(c, 413, "Слишком большой запрос")
		} else {
			Fail(c, 400, "Некорректная multipart-форма")
		}
		return
	}
	form := c.Request.MultipartForm
	for k, v := range form.Value {
		if k != "tariff_name" || len(v) != 1 {
			Fail(c, 400, "Разрешено только текстовое поле tariff_name; image и video должны быть файлами")
			return
		}
	}
	for k, v := range form.File {
		if (k != "image" && k != "video") || len(v) != 1 {
			Fail(c, 400, "Разрешено по одному файлу image и video")
			return
		}
	}
	name := strings.TrimSpace(c.PostForm("tariff_name"))
	if name == "" || utf8.RuneCountInString(name) > 100 {
		Fail(c, 400, "Название: от 1 до 100 символов")
		return
	}
	imgHeader, _ := c.FormFile("image")
	vidHeader, _ := c.FormFile("video")
	img, err := repository.ValidateUpload(c.Request.Context(), imgHeader, false)
	if err != nil {
		apiError(c, err)
		return
	}
	vid, err := repository.ValidateUpload(c.Request.Context(), vidHeader, true)
	if err != nil {
		apiError(c, err)
		return
	}
	t := ds.CloudTariff{TariffName: name, TariffStatus: ds.StatusDraft, CreatorID: currentuser.Get(c).ID()}
	t.Creator, err = h.Repository.GetUser(currentuser.Get(c).ID())
	if err != nil {
		apiError(c, err)
		return
	}
	if err = h.Repository.CreateWithFiles(c.Request.Context(), &t, img, vid); err != nil {
		apiError(c, err)
		return
	}
	success(c, 201, h.serialize(c.Request.Context(), t, currentuser.Get(c).ID()))
}

type publishRequest struct {
	Name        string `json:"tariff_name"`
	Description string `json:"short_description"`
	Price       *int   `json:"price_per_month"`
	RAM         *int   `json:"ram_gb"`
}

// PublishAPI godoc
// @Summary Опубликовать свой черновик
// @Tags Тарифы
// @Produce json
// @Param id path int true "Положительный ID" minimum(1)
// @Accept json
// @Param body body publishRequest true "Поля публикации"
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 200 {object} TariffEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs/{id}/publication [put]
func (h *Handler) PublishAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body publishRequest
	if !strictJSON(c, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Description = strings.TrimSpace(body.Description)
	if body.Name == "" || utf8.RuneCountInString(body.Name) > 100 || body.Description == "" || utf8.RuneCountInString(body.Description) > 500 || body.Price == nil || *body.Price < 0 || *body.Price > 2147483647 || body.RAM == nil || *body.RAM <= 0 || *body.RAM > 2147483647 {
		Fail(c, 400, "Нужны название (1–100), описание (1–500), цена >= 0 и RAM > 0 (целые до 2147483647)")
		return
	}
	err := h.Repository.PublishDraftTariff(repository.PublishTariffInput{TariffID: id, CreatorID: currentuser.Get(c).ID(), TariffName: body.Name, ShortDescription: body.Description, PricePerMonth: *body.Price, RAMGB: *body.RAM})
	if err != nil {
		apiError(c, err)
		return
	}
	t, err := h.Repository.GetPublishedTariff(id, currentuser.Get(c).ID())
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, h.serialize(c.Request.Context(), *t, currentuser.Get(c).ID()))
}

// DeleteAPI godoc
// @Summary Логически удалить свой тариф
// @Tags Тарифы
// @Produce json
// @Param id path int true "Положительный ID" minimum(1)
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 200 {object} DeleteEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs/{id} [delete]
func (h *Handler) DeleteAPI(c *gin.Context) {
	if !noQuery(c) || !noBody(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.Repository.DeleteTariff(c.Request.Context(), id, currentuser.Get(c).ID()); err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, gin.H{"deleted": true})
}

type likeRequest struct {
	Value *int `json:"value" example:"1"`
}

// LikeAPI godoc
// @Summary Поставить или отменить свой лайк
// @Tags Тарифы
// @Produce json
// @Param id path int true "Положительный ID" minimum(1)
// @Accept json
// @Param body body likeRequest true "1 — поставить, 0 — отменить"
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 200 {object} LikeEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs/{id}/likes [post]
func (h *Handler) LikeAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body likeRequest
	if !strictJSON(c, &body) {
		return
	}
	if body.Value == nil || (*body.Value != 0 && *body.Value != 1) {
		Fail(c, 400, "value должен быть 0 или 1")
		return
	}
	count, err := h.Repository.SetLike(c.Request.Context(), id, currentuser.Get(c).ID(), *body.Value)
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, gin.H{"tariff_id": id, "is_liked": *body.Value, "like_count": count})
}

// RegisterAPI godoc
// @Summary Зарегистрировать пользователя
// @Tags Пользователи
// @Produce json
// @Accept json
// @Param body body Credentials true "Логин 3–50 [a-z0-9_.-], пароль 8–72 байта"
// @Success 201 {object} UserEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /users [post]
func (h *Handler) RegisterAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	var body Credentials
	if !strictJSON(c, &body) {
		return
	}
	body.Login = strings.ToLower(strings.TrimSpace(body.Login))
	if len(body.Login) < 3 || len(body.Login) > 50 || len(body.Password) < 8 || len(body.Password) > 72 {
		Fail(c, 400, "Логин: 3–50 символов; пароль: 8–72 байта")
		return
	}
	for _, r := range body.Login {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			Fail(c, 400, "Логин: латинские буквы, цифры, _, - и .")
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		apiError(c, err)
		return
	}
	u := ds.User{Login: body.Login, PasswordHash: string(hash), IsModerator: false}
	if err = h.Repository.Register(&u); err != nil {
		apiError(c, err)
		return
	}
	success(c, 201, u)
}
