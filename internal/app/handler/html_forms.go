package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// HTMLForm adapts the response format for native browser forms. Business checks
// and repository operations remain in the same handlers used by the REST API.
func HTMLForm(action gin.HandlerFunc, destination, back string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("htmlForm", true)
		c.Set("htmlDestination", strings.ReplaceAll(destination, ":id", c.Param("id")))
		c.Set("htmlBack", strings.ReplaceAll(back, ":id", c.Param("id")))
		action(c)
	}
}

func allowedHTMLForm(c *gin.Context, fields ...string) bool {
	if c.ContentType() != "application/x-www-form-urlencoded" {
		Fail(c, http.StatusUnsupportedMediaType, "Ожидается HTML-форма")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.Request.ParseForm(); err != nil {
		Fail(c, http.StatusBadRequest, "Некорректная форма")
		return false
	}
	for name, values := range c.Request.PostForm {
		allowed := false
		for _, field := range fields {
			if name == field {
				allowed = true
				break
			}
		}
		if !allowed || len(values) != 1 {
			Fail(c, http.StatusBadRequest, "Недопустимое или повторяющееся поле: "+name)
			return false
		}
	}
	return true
}

func readPublication(c *gin.Context, body *publishRequest) bool {
	if !c.GetBool("htmlForm") {
		return strictJSON(c, body)
	}
	if !allowedHTMLForm(c, "tariff_name", "short_description", "price_per_month", "ram_gb") {
		return false
	}
	body.Name = c.Request.PostForm.Get("tariff_name")
	body.Description = c.Request.PostForm.Get("short_description")
	price, errPrice := strconv.ParseInt(c.Request.PostForm.Get("price_per_month"), 10, 32)
	ram, errRAM := strconv.ParseInt(c.Request.PostForm.Get("ram_gb"), 10, 32)
	if errPrice != nil || errRAM != nil {
		Fail(c, http.StatusBadRequest, "Цена и RAM должны быть целыми числами")
		return false
	}
	priceValue, ramValue := int(price), int(ram)
	body.Price, body.RAM = &priceValue, &ramValue
	return true
}

type likeRequest struct {
	Value *int `json:"value"`
}

func readLike(c *gin.Context, body *likeRequest) bool {
	if !c.GetBool("htmlForm") {
		return strictJSON(c, body)
	}
	if !allowedHTMLForm(c, "value") {
		return false
	}
	value, err := strconv.Atoi(c.Request.PostForm.Get("value"))
	if err != nil {
		Fail(c, http.StatusBadRequest, "value должен быть 0 или 1")
		return false
	}
	body.Value = &value
	return true
}
