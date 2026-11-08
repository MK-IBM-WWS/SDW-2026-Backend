package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"cloud-tariffs-backend/internal/app/currentuser"
	"cloud-tariffs-backend/internal/app/feed"
	"cloud-tariffs-backend/internal/app/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ListAPI godoc
// @Summary Страница опубликованных тарифов с фильтром цены
// @Tags Тарифы
// @Produce json
// @Description Сортировка price_per_month, tariff_id; query_ms — время COUNT и SELECT, без HTTP/MinIO. Максимум 100 карточек за запрос.
// @Param Authorization header string false "Bearer JWT"
// @Param priceLimit query int false "Максимальная цена" minimum(0)
// @Param page query int false "Страница" default(1) minimum(1)
// @Param limit query int false "Размер страницы" default(10) minimum(1) maximum(100)
// @Success 200 {object} ListEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /tariffs [get]
func (h *Handler) ListAPI(c *gin.Context) {
	query := c.Request.URL.Query()
	for k, v := range query {
		if (k != "priceLimit" && k != "page" && k != "limit") || len(v) != 1 {
			Fail(c, 400, "Допустимы однократные priceLimit, page, limit")
			return
		}
	}
	page, limit := 1, 10
	for k, target := range map[string]*int{"page": &page, "limit": &limit} {
		if values, ok := query[k]; ok {
			n, err := strconv.ParseInt(values[0], 10, 32)
			if err != nil || n < 1 || (k == "limit" && n > 100) {
				Fail(c, 400, "page >= 1, limit от 1 до 100 (целые числа)")
				return
			}
			*target = int(n)
		}
	}
	var maxPrice *int
	if v, ok := query["priceLimit"]; ok {
		n, e := strconv.ParseInt(v[0], 10, 32)
		if e != nil || n < 0 {
			Fail(c, 400, "priceLimit должен быть целым числом >= 0")
			return
		}
		value := int(n)
		maxPrice = &value
	}
	offset64 := int64(page-1) * int64(limit)
	if offset64 > 2147483647 {
		Fail(c, 400, "Слишком большой номер страницы")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, total, ms, err := h.Repository.GetPublishedTariffs(ctx, maxPrice, currentuser.Get(c).ID(), limit, int(offset64))
	if err != nil {
		apiError(c, err)
		return
	}
	enabled, err := h.Repository.PriceIndexEnabled(ctx)
	if err != nil {
		apiError(c, err)
		return
	}
	result := make([]ListTariffResponse, 0, len(list))
	for _, row := range list {
		result = append(result, h.serializeList(ctx, row, currentuser.Get(c).ID()))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, ListEnvelope{Status: "success", Data: result, Total: total, Page: page, Limit: limit, QueryMS: ms, IndexEnabled: enabled})
}

// FeedAPI godoc
// @Summary Карточка случайной ленты из Redis
// @Tags Тарифы
// @Produce json
// @Description При первом обращении создаёт 100 уникальных карточек. Сессия feed_session — HttpOnly cookie без авторизации, TTL Redis 24 часа. position — сквозной номер. init не продвигает ленту.
// @Param Authorization header string false "Bearer JWT"
// @Param position query int false "Порядковый номер в текущем наборе или сразу после него" minimum(1)
// @Param init query boolean false "Сформировать/восстановить ленту при старте приложения"
// @Param id query int false "Открыть карточку из плитки" minimum(1)
// @Param next query boolean false "Следующая в Redis-порядке после id"
// @Success 200 {object} FeedEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Failure 503 {object} ErrorEnvelope
// @Router /tariffs/feed [get]
func (h *Handler) FeedAPI(c *gin.Context) {
	query := c.Request.URL.Query()
	q := feed.Query{}
	for k, v := range query {
		if (k != "position" && k != "id" && k != "next" && k != "init") || len(v) != 1 {
			Fail(c, 400, "Допустимы однократные position, id, next, init")
			return
		}
	}
	_, hasID := query["id"]
	_, hasNext := query["next"]
	_, hasPos := query["position"]
	_, hasInit := query["init"]
	if (hasNext && !hasID) || (hasPos && (hasID || hasNext || hasInit)) || (hasInit && (hasID || hasNext)) {
		Fail(c, 400, "Не совмещайте position/init с id; next требует id")
		return
	}
	if hasID {
		n, e := strconv.ParseUint(query.Get("id"), 10, 32)
		if e != nil || n == 0 {
			Fail(c, 400, "id должен быть положительным целым числом")
			return
		}
		q.ID = uint(n)
	}
	if hasPos {
		n, e := strconv.ParseInt(query.Get("position"), 10, 32)
		if e != nil || n < 1 {
			Fail(c, 400, "position должен быть положительным целым числом")
			return
		}
		q.Position = int(n)
	}
	for k, target := range map[string]*bool{"next": &q.Next, "init": &q.Init} {
		if v, ok := query[k]; ok {
			if v[0] != "true" && v[0] != "false" {
				Fail(c, 400, k+" должен быть true или false")
				return
			}
			*target = v[0] == "true"
		}
	}
	if h.Feed == nil {
		Fail(c, 503, "Хранилище ленты недоступно")
		return
	}
	session, _ := c.Cookie("feed_session")
	if _, e := uuid.Parse(session); e != nil {
		session = uuid.NewString()
		http.SetCookie(c.Writer, &http.Cookie{Name: "feed_session", Value: session, Path: "/api", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteLaxMode})
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	row, meta, err := h.Feed.Card(ctx, session, currentuser.Get(c).ID(), q)
	if errors.Is(err, feed.ErrUnavailable) {
		Fail(c, 503, "Redis недоступен. Лента не сформирована.")
		return
	}
	if errors.Is(err, repository.ErrTariffNotFound) && q.ID == 0 {
		row = nil
		err = nil
	}
	if err != nil {
		apiError(c, err)
		return
	}
	var data *TariffResponse
	if row != nil {
		dto := h.serialize(ctx, *row, currentuser.Get(c).ID())
		data = &dto
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, FeedEnvelope{Status: "success", Data: data, Feed: meta})
}
