package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Optional extension keeps the ten original lab API methods and their contracts intact.
type nativeRepository interface {
	NativeCatalog(context.Context, string, *int, bool, int, int) ([]ds.CloudTariff, int64, error)
	OpenNativeImage(context.Context, string) (io.ReadCloser, string, error)
}

type nativeTariff struct {
	ID          uint   `json:"tariff_id"`
	Name        string `json:"tariff_name"`
	Description string `json:"short_description"`
	Price       int    `json:"price_per_month"`
	RAM         int    `json:"ram_gb"`
	Image       string `json:"image_url"`
	Likes       int64  `json:"like_count"`
}

func nativeDTO(row ds.CloudTariff) nativeTariff {
	return nativeTariff{row.TariffID, row.TariffName, row.ShortDescription, row.PricePerMonth, row.RAMGB, "/api/native/tariffs/" + strconv.FormatUint(uint64(row.TariffID), 10) + "/image", row.LikeCount}
}

func (h *Handler) NativeListAPI(c *gin.Context) {
	repo, ok := h.Repository.(nativeRepository)
	if !ok {
		Fail(c, 503, "Гостевой каталог недоступен")
		return
	}
	query := c.Request.URL.Query()
	for k, v := range query {
		if len(v) != 1 || (k != "q" && k != "priceLimit" && k != "page" && k != "limit" && k != "demo" && k != "semantic") {
			Fail(c, 400, "Допустимы q, priceLimit, page, limit, demo, semantic без повторов")
			return
		}
	}
	q := strings.TrimSpace(query.Get("q"))
	if len([]rune(q)) > 100 {
		Fail(c, 400, "Поиск: максимум 100 символов")
		return
	}
	page, limit := 1, 100
	for key, dst := range map[string]*int{"page": &page, "limit": &limit} {
		if val, exists := query[key]; exists {
			n, err := strconv.ParseInt(val[0], 10, 32)
			if err != nil || n < 1 || (key == "limit" && n > 100) {
				Fail(c, 400, "page >= 1; limit от 1 до 100")
				return
			}
			*dst = int(n)
		}
	}
	if int64(page-1)*int64(limit) > 2147483647 {
		Fail(c, 400, "Слишком большой номер страницы")
		return
	}
	var price *int
	if value, exists := query["priceLimit"]; exists {
		n, err := strconv.ParseInt(value[0], 10, 32)
		if err != nil || n < 0 {
			Fail(c, 400, "priceLimit >= 0")
			return
		}
		v := int(n)
		price = &v
	}
	demo := query.Get("demo")
	if demo != "" && demo != "true" && demo != "false" {
		Fail(c, 400, "demo должен быть true/false")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	var rows []ds.CloudTariff
	var total int64
	var err error
	semantic := query.Get("semantic")
	if semantic != "" && semantic != "true" && semantic != "false" {
		Fail(c, 400, "semantic должен быть true/false")
		return
	}
	if semantic == "true" {
		searchRepo, ok := h.Repository.(interface {
			NativeSearchCatalog(context.Context, string, int, int) ([]ds.CloudTariff, int64, error)
		})
		if !ok {
			Fail(c, 503, "Поиск по описаниям недоступен")
			return
		}
		rows, total, err = searchRepo.NativeSearchCatalog(ctx, q, limit, (page-1)*limit)
	} else {
		rows, total, err = repo.NativeCatalog(ctx, q, price, demo == "true", limit, (page-1)*limit)
	}
	if err != nil {
		apiError(c, err)
		return
	}
	result := make([]nativeTariff, 0, len(rows))
	for _, row := range rows {
		result = append(result, nativeDTO(row))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "success", "data": result, "total": total, "page": page, "limit": limit})
}
func (h *Handler) NativeDetailAPI(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.Repository.GetPublishedTariff(id, 0)
	if err != nil {
		apiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	success(c, 200, nativeDTO(*row))
}
func (h *Handler) NativeImageAPI(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	row, err := h.Repository.GetPublishedTariff(id, 0)
	if err != nil {
		apiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	if row.ImageURL != "" {
		if repo, ok := h.Repository.(nativeRepository); ok {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
			defer cancel()
			image, typ, err := repo.OpenNativeImage(ctx, row.ImageURL)
			if err == nil {
				defer image.Close()
				if !strings.HasPrefix(typ, "image/") {
					typ = "application/octet-stream"
				}
				c.Header("Content-Type", typ)
				c.Header("X-Content-Type-Options", "nosniff")
				_, _ = io.Copy(c.Writer, image)
				return
			}
		}
	}
	c.File("./resources/media/example.jpg")
}
