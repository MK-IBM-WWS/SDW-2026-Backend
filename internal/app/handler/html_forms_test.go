package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"github.com/gin-gonic/gin"
)

type htmlRepo struct {
	Repository
	tariff   *ds.CloudTariff
	limit    *int
	uploaded bool
}

func (r *htmlRepo) GetUser(id uint) (ds.User, error) {
	return ds.User{UserID: id, Login: "student"}, nil
}
func (r *htmlRepo) GetDraftTariff(id uint) (*ds.CloudTariff, error) {
	if r.tariff == nil || r.tariff.TariffStatus != ds.StatusDraft {
		return nil, nil
	}
	clone := *r.tariff
	return &clone, nil
}
func (r *htmlRepo) CreateWithFiles(_ context.Context, t *ds.CloudTariff, img, vid *repository.Upload) error {
	if r.tariff != nil && r.tariff.TariffStatus == ds.StatusDraft {
		return repository.ErrDraftExists
	}
	t.TariffID = 7
	t.CreatedAt = time.Now()
	r.uploaded = img != nil && vid != nil
	clone := *t
	r.tariff = &clone
	return nil
}
func (r *htmlRepo) PublishDraftTariff(in repository.PublishTariffInput) error {
	if in.CreatorID != r.tariff.CreatorID {
		return repository.ErrForbidden
	}
	if r.tariff.TariffStatus != ds.StatusDraft {
		return repository.ErrInvalidState
	}
	r.tariff.TariffStatus = ds.StatusPublished
	r.tariff.PricePerMonth = in.PricePerMonth
	r.tariff.RAMGB = in.RAMGB
	r.tariff.TariffName = in.TariffName
	r.tariff.ShortDescription = in.ShortDescription
	return nil
}
func (r *htmlRepo) GetPublishedTariff(id uint) (*ds.CloudTariff, error) {
	if r.tariff == nil || r.tariff.TariffStatus != ds.StatusPublished {
		return nil, repository.ErrTariffNotFound
	}
	clone := *r.tariff
	return &clone, nil
}
func (r *htmlRepo) GetPublishedTariffs(limit *int) ([]ds.CloudTariff, error) {
	r.limit = limit
	if r.tariff == nil || r.tariff.TariffStatus != ds.StatusPublished {
		return []ds.CloudTariff{}, nil
	}
	clone := *r.tariff
	clone.IsOwner = 1
	return []ds.CloudTariff{clone}, nil
}
func (r *htmlRepo) SetLike(_ context.Context, id uint, value int) (int64, error) {
	if r.tariff.TariffStatus != ds.StatusPublished {
		return 0, repository.ErrTariffNotFound
	}
	r.tariff.IsLiked = value
	r.tariff.LikeCount = int64(value)
	return int64(value), nil
}
func (r *htmlRepo) DeleteTariff(_ context.Context, id uint) error {
	if r.tariff.CreatorID != 1 {
		return repository.ErrForbidden
	}
	r.tariff.TariffStatus = ds.StatusDeleted
	return nil
}
func htmlRouter(r *htmlRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewHandler(r)
	router := gin.New()
	router.LoadHTMLGlob("../../../templates/*.html")
	router.GET("/tariffs", h.GetTariffTiles)
	router.GET("/tariffs/draft", h.GetTariffDraft)
	router.GET("/tariffs/feed", h.GetTariffFeed)
	router.POST("/tariffs/draft", HTMLForm(h.CreateAPI, "/tariffs/draft", "/tariffs/draft"))
	router.POST("/tariffs/:id/publication", HTMLForm(h.PublishAPI, "/tariffs/feed?id=:id", "/tariffs/draft"))
	router.POST("/tariffs/:id/likes", HTMLForm(h.LikeAPI, "/tariffs/feed?id=:id", "/tariffs/feed?id=:id"))
	router.POST("/tariffs/:id/delete", HTMLForm(h.DeleteAPI, "/tariffs", "/tariffs"))
	return router
}
func formRequest(router *gin.Engine, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
func TestHTMLLifecycleWithoutJavaScript(t *testing.T) {
	r := &htmlRepo{}
	router := htmlRouter(r)
	w := formRequest(router, "GET", "/tariffs/draft", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `action="/tariffs/draft" method="POST" enctype="multipart/form-data"`) {
		t.Fatal("missing native upload form")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("tariff_name", "HTML tariff")
	for field, name := range map[string]string{"image": "example.jpg", "video": "example.mp4"} {
		file, err := os.Open(filepath.Join("../../../resources/media", name))
		if err != nil {
			t.Fatal(err)
		}
		part, err := writer.CreateFormFile(field, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.Copy(part, file); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	writer.Close()
	w = formRequest(router, "POST", "/tariffs/draft", body.String(), writer.FormDataContentType())
	if w.Code != 303 || w.Header().Get("Location") != "/tariffs/draft" || !r.uploaded || r.tariff.CreatorID != 1 {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	w = formRequest(router, "GET", "/tariffs/draft", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `action="/tariffs/7/publication" method="POST"`) {
		t.Fatal("missing publication form")
	}
	fields := url.Values{"tariff_name": {"HTML tariff"}, "short_description": {"Test description"}, "price_per_month": {"900"}, "ram_gb": {"8"}}
	for _, bad := range []string{"creator_id", "tariff_status", "formed_at", "image_url"} {
		fields.Set(bad, "1")
		w = formRequest(router, "POST", "/tariffs/7/publication", fields.Encode(), "application/x-www-form-urlencoded")
		if w.Code != 400 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") || r.tariff.TariffStatus != ds.StatusDraft {
			t.Fatal("protected field accepted in HTML form")
		}
		fields.Del(bad)
	}
	w = formRequest(router, "POST", "/tariffs/7/publication", fields.Encode(), "application/x-www-form-urlencoded")
	if w.Code != 303 || w.Header().Get("Location") != "/tariffs/feed?id=7" || r.tariff.RAMGB != 8 {
		t.Fatalf("publish failed: %d %s", w.Code, w.Body.String())
	}
	w = formRequest(router, "GET", "/tariffs?priceStep=1", "", "")
	if w.Code != 200 || r.limit == nil || *r.limit != 1000 || !strings.Contains(w.Body.String(), `action="/tariffs/7/delete" method="POST"`) {
		t.Fatal("filter/delete form failed")
	}
	for _, value := range []string{"1", "0"} {
		w = formRequest(router, "POST", "/tariffs/7/likes", "value="+value, "application/x-www-form-urlencoded")
		if w.Code != 303 || w.Header().Get("Location") != "/tariffs/feed?id=7" {
			t.Fatal("like failed")
		}
		w = formRequest(router, "GET", "/tariffs/feed?id=7", "", "")
		expected := `name="value" value="1"`
		if value == "1" {
			expected = `name="value" value="0"`
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), expected) {
			t.Fatal("like toggle form incorrect")
		}
	}
	w = formRequest(router, "GET", "/tariffs/feed?id=7&fallback=true", "", "")
	if !strings.Contains(w.Body.String(), `<source src="/static/media/example.mp4">`) {
		t.Fatal("manual fallback failed")
	}
	r.tariff.CreatorID = 2
	w = formRequest(router, "POST", "/tariffs/7/delete", "", "application/x-www-form-urlencoded")
	if w.Code != 403 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatal("foreign delete must fail as HTML")
	}
	r.tariff.CreatorID = 1
	w = formRequest(router, "POST", "/tariffs/7/delete", "", "application/x-www-form-urlencoded")
	if w.Code != 303 || r.tariff.TariffStatus != ds.StatusDeleted {
		t.Fatal("soft delete failed")
	}
	for _, path := range []string{"/tariffs?priceStep=-1", "/tariffs?priceStep=4", "/tariffs?priceStep=a"} {
		if w = formRequest(router, "GET", path, "", ""); w.Code != 400 {
			t.Fatal("invalid slider accepted")
		}
	}
}
func TestTemplatesContainNoJavaScript(t *testing.T) {
	files, err := filepath.Glob("../../../templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(data))
		for _, bad := range []string{"<script", "javascript:", "onclick=", "onerror=", "onchange=", "onsubmit="} {
			if strings.Contains(text, bad) {
				t.Fatalf("%s contains %s", path, bad)
			}
		}
	}
}
