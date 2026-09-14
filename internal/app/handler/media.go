package handler

import (
	"net/http"
	"os"
	"strings"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
)

const (
	defaultFallbackImageURL = "http://localhost:9000/cloud-tariffs/example.jpg"
	defaultFallbackVideoURL = "http://localhost:9000/cloud-tariffs/example.mp4"
)

var mediaHTTPClient = &http.Client{Timeout: 2 * time.Second}

// applyMediaFallbacks подставляет стандартные объекты MinIO только для отображения.
// URL, сохранённые в PostgreSQL, при этом не изменяются.
func (h *Handler) applyMediaFallbacks(tariff *ds.CloudTariff) {
	if tariff == nil {
		return
	}

	if !mediaExists(tariff.ImageURL) {
		tariff.ImageURL = fallbackURL("MINIO_FALLBACK_IMAGE_URL", defaultFallbackImageURL)
	}
	if !mediaExists(tariff.VideoURL) {
		tariff.VideoURL = fallbackURL("MINIO_FALLBACK_VIDEO_URL", defaultFallbackVideoURL)
	}
}

func mediaExists(url string) bool {
	url = strings.TrimSpace(url)
	if url == "" {
		return false
	}

	request, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false
	}

	response, err := mediaHTTPClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()

	return response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusBadRequest
}

func fallbackURL(envName, defaultURL string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		return value
	}
	return defaultURL
}
