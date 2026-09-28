package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

type missingMediaRepository struct {
	Repository
	tariff ds.CloudTariff
	hidden bool
}

func (r missingMediaRepository) VisibleTariff(uint, uint) (*ds.CloudTariff, error) {
	if r.hidden {
		return nil, repository.ErrTariffNotFound
	}
	return &r.tariff, nil
}
func (r missingMediaRepository) OpenMedia(context.Context, string) (*minio.Object, minio.ObjectInfo, error) {
	return nil, minio.ObjectInfo{}, errors.New("missing object")
}
func TestMissingMediaUsesStaticURLs(t *testing.T) {
	dto := serialize(ds.CloudTariff{TariffID: 1})
	if dto.ImageURL != fallbackImageURL || dto.VideoURL != fallbackVideoURL {
		t.Fatal("empty media must use static URLs")
	}
	for _, hasName := range []bool{false, true} {
		tariff := ds.CloudTariff{TariffID: 1}
		if hasName {
			tariff.ImageURL = "missing.jpg"
			tariff.VideoURL = "missing.mp4"
		}
		for kind, fallback := range map[string]string{"image": fallbackImageURL, "video": fallbackVideoURL} {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.GET("/api/tariffs/:id/"+kind, NewHandler(missingMediaRepository{tariff: tariff}).MediaAPI)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("GET", "/api/tariffs/1/"+kind, nil))
			if w.Code != 302 || w.Header().Get("Location") != fallback {
				t.Fatalf("%s: %d %s", kind, w.Code, w.Header().Get("Location"))
			}
		}
	}
}
func TestHiddenMediaStillReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/tariffs/:id/image", NewHandler(missingMediaRepository{hidden: true}).MediaAPI)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/tariffs/1/image", nil))
	if w.Code != 404 || w.Header().Get("Location") != "" {
		t.Fatal("visibility must be checked before media fallback")
	}
}
