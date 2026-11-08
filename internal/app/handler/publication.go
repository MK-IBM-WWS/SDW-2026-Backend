package handler

import (
	"cloud-tariffs-backend/internal/app/repository"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// Старые JSON-запросы без файлов остаются совместимыми.
// Форма с файлами меняет медиа и поля того же черновика при публикации.
func parsePublication(c *gin.Context) (publishRequest, *repository.Upload, *repository.Upload, bool) {
	var body publishRequest
	if c.ContentType() != "multipart/form-data" {
		return bodyFromJSON(c)
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 62<<20)
	err := c.Request.ParseMultipartForm(2 << 20)
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			Fail(c, 413, "Слишком большой запрос")
		} else {
			Fail(c, 400, "Некорректная multipart-форма")
		}
		return body, nil, nil, false
	}
	form := c.Request.MultipartForm
	allowed := map[string]bool{"tariff_name": true, "short_description": true, "price_per_month": true, "ram_gb": true}
	for k, v := range form.Value {
		if !allowed[k] || len(v) != 1 {
			Fail(c, 400, "Некорректные поля публикации")
			return body, nil, nil, false
		}
	}
	for k, v := range form.File {
		if (k != "image" && k != "video") || len(v) != 1 {
			Fail(c, 400, "Разрешено по одному файлу image и video")
			return body, nil, nil, false
		}
	}
	price, priceErr := strconv.Atoi(c.PostForm("price_per_month"))
	ram, ramErr := strconv.Atoi(c.PostForm("ram_gb"))
	if priceErr != nil || ramErr != nil {
		Fail(c, 400, "Цена и RAM должны быть целыми числами")
		return body, nil, nil, false
	}
	body = publishRequest{Name: c.PostForm("tariff_name"), Description: c.PostForm("short_description"), Price: &price, RAM: &ram}
	imgHeader, _ := c.FormFile("image")
	vidHeader, _ := c.FormFile("video")
	img, err := repository.ValidateUpload(c.Request.Context(), imgHeader, false)
	if err != nil {
		apiError(c, err)
		return body, nil, nil, false
	}
	vid, err := repository.ValidateUpload(c.Request.Context(), vidHeader, true)
	if err != nil {
		apiError(c, err)
		return body, nil, nil, false
	}
	return body, img, vid, true
}

func bodyFromJSON(c *gin.Context) (publishRequest, *repository.Upload, *repository.Upload, bool) {
	var body publishRequest
	ok := strictJSON(c, &body)
	return body, nil, nil, ok
}
