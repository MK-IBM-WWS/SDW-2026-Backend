// ЛР4. Этот файл выполняется командой go test; обычный сервер его не запускает.
// Подмены хранилищ позволяют проверять логику без настоящих контейнеров Docker.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"github.com/gin-gonic/gin"
)

// Вспомогательная функция теста: готовит окружение, запрос или ответ подмены.
func call(t *testing.T, method, path, body string, fn gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, "/api/tariffs/:id", fn)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Проверяем, что клиент не может задать автора, статус и другие служебные поля через JSON публикации.
func TestProtectedFieldsRejected(t *testing.T) {
	h := NewHandler(nil)
	for _, field := range []string{"id", "tariff_id", "tariff_status", "creator_id", "moderator_id", "created_at", "formed_at", "completed_at", "image_url", "video_url"} {
		t.Run(field, func(t *testing.T) {
			body := `{"tariff_name":"Test","short_description":"desc","price_per_month":10,"ram_gb":4,"` + field + `":1}`
			w := call(t, "PUT", "/api/tariffs/1", body, h.PublishAPI)
			if w.Code != 400 {
				t.Fatalf("%s: %d %s", field, w.Code, w.Body.String())
			}
		})
	}
}

// Проверяем отказ для плохих ID и значений лайка, отличных от 0/1.
func TestInvalidIDsAndLikeBodies(t *testing.T) {
	h := NewHandler(nil)
	for _, id := range []string{"-1", "0", "abc", "99999999999999999999999999999"} {
		w := call(t, "DELETE", "/api/tariffs/"+id, "", h.DeleteAPI)
		if w.Code != 400 {
			t.Fatalf("id %s: %d", id, w.Code)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"value":null}`, `{"value":2}`, `{"value":-1}`, `{"value":"1"}`, `{"value":true}`, `{"value":1,"user_id":2}`, `{"value":1} {"value":0}`} {
		w := call(t, "POST", "/api/tariffs/1", body, h.LikeAPI)
		if w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
	w := call(t, "DELETE", "/api/tariffs/1", `{"creator_id":2}`, h.DeleteAPI)
	if w.Code != 400 {
		t.Fatal("delete body accepted")
	}
}

// Проверяем, что JSON тарифа не раскрывает пароль и содержит правильный признак владельца.
func TestSerializationDoesNotExposePassword(t *testing.T) {
	b, err := json.Marshal(NewHandler(&mediaRepository{}).serialize(context.Background(), ds.CloudTariff{TariffID: 7, CreatorID: 1, ImageURL: "image_test.jpg", VideoURL: "video_test.mp4", Creator: ds.User{UserID: 1, Login: "student", PasswordHash: "secret"}}, 1))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("secret")) || bytes.Contains(b, []byte("PasswordHash")) {
		t.Fatal("password leaked")
	}
	var v map[string]any
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v["is_owner"] != float64(1) || v["image_url"] != "http://minio.example/test-bucket/image_test.jpg?X-Amz-Signature=test" {
		t.Fatalf("bad DTO: %s", b)
	}
}

// Проверяем настоящие байты изображения и видео, даже если клиент указал странное имя файла.
func TestUploadValidation(t *testing.T) {
	for _, tt := range []struct {
		name, path string
		video      bool
	}{{"image", "../../../resources/media/example.jpg", false}, {"video", "../../../resources/media/example.mp4", true}} {
		t.Run(tt.name, func(t *testing.T) {
			content, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			writer := multipart.NewWriter(&b)
			part, _ := writer.CreateFormFile("file", "имя.exe")
			part.Write(content)
			writer.Close()
			req := httptest.NewRequest("POST", "/", &b)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err = req.ParseMultipartForm(2 << 20); err != nil {
				t.Fatal(err)
			}
			defer req.MultipartForm.RemoveAll()
			header := req.MultipartForm.File["file"][0]
			u, err := repository.ValidateUpload(req.Context(), header, tt.video)
			if err != nil || u == nil {
				t.Fatalf("valid file rejected: %v", err)
			}
		})
	}
}

// Проверяем, что URL, переданный текстом в поле image, не заменяет загрузку файла.
func TestTextInsteadOfFileRejected(t *testing.T) {
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	writer.WriteField("tariff_name", "Test")
	writer.WriteField("image", "https://example.com/pic.jpg")
	writer.Close()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/tariffs", NewHandler(nil).CreateAPI)
	req := httptest.NewRequest("POST", "/api/tariffs", &b)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("text image accepted: %d", w.Code)
	}
}

// Проверяем соответствие ошибок репозитория кодам HTTP.
func TestErrorStatuses(t *testing.T) {
	for _, tt := range []struct {
		err  error
		code int
	}{{repository.ErrDraftExists, 409}, {repository.ErrForbidden, 403}, {repository.ErrTariffNotFound, 404}, {repository.Invalid("bad"), 400}} {
		w := call(t, http.MethodGet, "/api/tariffs/1", "", func(c *gin.Context) { apiError(c, tt.err) })
		if w.Code != tt.code {
			t.Fatal(w.Code)
		}
	}
}
