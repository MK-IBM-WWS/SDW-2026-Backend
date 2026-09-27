package handler

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
)

const (
	defaultMinIOBaseURL = "http://localhost:9000/cloud-tariffs/"
	fallbackImageURL   = "/static/media/example.jpg"
	fallbackVideoURL   = "/static/media/example.mp4"
)

var mediaHTTPClient = &http.Client{Timeout: 2 * time.Second}

func (h *Handler) applyMediaFallbacks(tariff *ds.CloudTariff) {
	if tariff == nil {
		return
	}

	imageURL := minIOURL(tariff.ImageURL)
	videoURL := minIOURL(tariff.VideoURL)
	if mediaExists(imageURL) {
		tariff.ImageURL = imageURL
	} else {
		tariff.ImageURL = fallbackImageURL
	}
	if mediaExists(videoURL) {
		tariff.VideoURL = videoURL
	} else {
		tariff.VideoURL = fallbackVideoURL
	}
}

// Supports old full MinIO URLs and new file names saved in the database.
func minIOURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if strings.ContainsAny(value, `/\\`) || value == "." || value == ".." {
		return ""
	}
	base := strings.TrimSpace(os.Getenv("MINIO_MEDIA_BASE_URL"))
	if base == "" {
		base = defaultMinIOBaseURL
	}
	return strings.TrimRight(base, "/") + "/" + url.PathEscape(value)
}

func mediaExists(mediaURL string) bool {
	if mediaURL == "" {
		return false
	}

	request, err := http.NewRequest(http.MethodHead, mediaURL, nil)
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
