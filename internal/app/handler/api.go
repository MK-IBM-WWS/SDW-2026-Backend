package handler

import (
	"encoding/json"
	"errors"
	"fmt"
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

type TariffResponse struct {
	ds.CloudTariff
	ImageURL string  `json:"image_url"`
	VideoURL string  `json:"video_url"`
	Creator  ds.User `json:"creator"`
}

func serialize(t ds.CloudTariff) TariffResponse {
	if t.CreatorID == currentuser.Get().ID() {
		t.IsOwner = 1
	}
	imageURL, videoURL := mediaURLs(t)
	return TariffResponse{CloudTariff: t, ImageURL: imageURL, VideoURL: videoURL, Creator: t.Creator}
}
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
	list, err := h.Repository.GetPublishedTariffs(max)
	if err != nil {
		apiError(c, err)
		return
	}
	result := make([]TariffResponse, 0, len(list))
	for _, t := range list {
		result = append(result, serialize(t))
	}
	success(c, 200, result)
}
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
		tariff, err = h.Repository.GetPublishedTariff(uint(id))
	} else {
		// Without parameters: the first card. With next/after_id: the next card,
		// wrapping to the first published card after the end of the feed.
		tariff, err = h.Repository.GetNextPublishedTariff(uint(id))
	}
	if errors.Is(err, repository.ErrTariffNotFound) && !exactCard {
		success(c, 200, nil)
		return
	}
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, serialize(*tariff))
}
func (h *Handler) DetailAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	t, err := h.Repository.GetPublishedTariff(id)
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, serialize(*t))
}
func (h *Handler) DraftAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	t, err := h.Repository.GetDraftTariff(currentuser.Get().ID())
	if err != nil {
		apiError(c, err)
		return
	}
	if t == nil {
		success(c, 200, nil)
		return
	}
	success(c, 200, serialize(*t))
}
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
	t := ds.CloudTariff{TariffName: name, TariffStatus: ds.StatusDraft, CreatorID: currentuser.Get().ID()}
	t.Creator, err = h.Repository.GetUser(currentuser.Get().ID())
	if err != nil {
		apiError(c, err)
		return
	}
	if err = h.Repository.CreateWithFiles(c.Request.Context(), &t, img, vid); err != nil {
		apiError(c, err)
		return
	}
	c.Header("Location", "/api/tariffs/draft")
	success(c, 201, serialize(t))
}

type publishRequest struct {
	Name        string `json:"tariff_name"`
	Description string `json:"short_description"`
	Price       *int   `json:"price_per_month"`
	RAM         *int   `json:"ram_gb"`
}

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
	err := h.Repository.PublishDraftTariff(repository.PublishTariffInput{TariffID: id, CreatorID: currentuser.Get().ID(), TariffName: body.Name, ShortDescription: body.Description, PricePerMonth: *body.Price, RAMGB: *body.RAM})
	if err != nil {
		apiError(c, err)
		return
	}
	t, err := h.Repository.GetPublishedTariff(id)
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, serialize(*t))
}
func (h *Handler) DeleteAPI(c *gin.Context) {
	if !noQuery(c) || !noBody(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.Repository.DeleteTariff(c.Request.Context(), id); err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, gin.H{"deleted": true})
}
func (h *Handler) LikeAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	var body struct {
		Value *int `json:"value"`
	}
	if !strictJSON(c, &body) {
		return
	}
	if body.Value == nil || (*body.Value != 0 && *body.Value != 1) {
		Fail(c, 400, "value должен быть 0 или 1")
		return
	}
	count, err := h.Repository.SetLike(c.Request.Context(), id, *body.Value)
	if err != nil {
		apiError(c, err)
		return
	}
	success(c, 200, gin.H{"tariff_id": id, "is_liked": *body.Value, "like_count": count})
}
func (h *Handler) RegisterAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
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
	c.Header("Location", fmt.Sprintf("/api/users/%d/tariffs", u.UserID))
	success(c, 201, u)
}
func (h *Handler) LoginAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !strictJSON(c, &body) {
		return
	}
	if strings.TrimSpace(body.Login) == "" || body.Password == "" {
		Fail(c, 400, "Нужны login и password")
		return
	}
	success(c, 200, gin.H{"stub": true, "authenticated": false, "current_user_id": currentuser.Get().ID(), "message": "Заглушка ЛР3: пароль не проверяется, сессия не создаётся, пользователь не меняется"})
}
func (h *Handler) LogoutAPI(c *gin.Context) {
	if !noQuery(c) || !noBody(c) {
		return
	}
	success(c, 200, gin.H{"stub": true, "current_user_id": currentuser.Get().ID(), "message": "Заглушка ЛР3: сессии отсутствуют, пользователь не меняется"})
}
func (h *Handler) UserTariffsAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	id, ok := parseID(c)
	if !ok {
		return
	}
	u, ts, err := h.Repository.UserTariffs(id)
	if err != nil {
		apiError(c, err)
		return
	}
	list := make([]TariffResponse, 0, len(ts))
	for _, t := range ts {
		list = append(list, serialize(t))
	}
	success(c, 200, gin.H{"user": u, "tariffs": list})
}
func (h *Handler) MediaAPI(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	t, err := h.Repository.VisibleTariff(id, currentuser.Get().ID())
	if err != nil {
		apiError(c, err)
		return
	}
	video := strings.HasSuffix(c.FullPath(), "/video")
	name := t.ImageURL
	fallback := fallbackImageURL
	if video {
		name = t.VideoURL
		fallback = fallbackVideoURL
	}
	if name == "" {
		c.Redirect(http.StatusFound, fallback)
		return
	}
	obj, info, err := h.Repository.OpenMedia(c.Request.Context(), name)
	if err != nil {
		c.Redirect(http.StatusFound, fallback)
		return
	}
	defer obj.Close()
	c.Header("Content-Type", info.ContentType)
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, name, info.LastModified, obj)
}
