package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicationMultipartParsing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"large_image", "no_files", "invalid_image", "duplicate", "invalid_integer", "unknown_field", "file_as_text"} {
		t.Run(mode, func(t *testing.T) {
			var payload bytes.Buffer
			writer := multipart.NewWriter(&payload)
			writer.WriteField("tariff_name", "Edited draft")
			writer.WriteField("short_description", "Description")
			price := "750"
			if mode == "invalid_integer" {
				price = "1.5"
			}
			writer.WriteField("price_per_month", price)
			writer.WriteField("ram_gb", "8")
			if mode == "unknown_field" {
				writer.WriteField("creator_id", "2")
			}
			if mode == "file_as_text" {
				writer.WriteField("image", "fake.jpg")
			}
			if mode == "duplicate" {
				writer.WriteField("tariff_name", "Second name")
			}
			if mode == "large_image" || mode == "invalid_image" {
				part, _ := writer.CreateFormFile("image", "new.png")
				if mode == "large_image" {
					part.Write([]byte{137, 80, 78, 71, 13, 10, 26, 10})
					part.Write(bytes.Repeat([]byte{0}, 3<<20))
				} else {
					part.Write([]byte("not an image"))
				}
			}
			writer.Close()
			req := httptest.NewRequest("PUT", "/", &payload)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			body, img, vid, ok := parsePublication(c)
			if req.MultipartForm != nil {
				defer req.MultipartForm.RemoveAll()
			}
			expected := mode == "large_image" || mode == "no_files"
			if ok != expected {
				t.Fatalf("result %v HTTP %d: %s", ok, w.Code, w.Body.String())
			}
			if !expected {
				if w.Code != 400 {
					t.Fatalf("want validation HTTP400, got %d", w.Code)
				}
				return
			}
			if body.Name != "Edited draft" || body.Price == nil || *body.Price != 750 || vid != nil {
				t.Fatal("incorrect fields")
			}
			if mode == "large_image" {
				if img == nil {
					t.Fatal("missing file")
				}
				f, err := img.Header.Open()
				if err != nil {
					t.Fatalf("temp file removed before upload: %v", err)
				}
				defer f.Close()
				buf, err := io.ReadAll(f)
				if err != nil || len(buf) != (3<<20)+8 {
					t.Fatal("file damaged")
				}
			} else if img != nil {
				t.Fatal("unexpected file")
			}
		})
	}
}
