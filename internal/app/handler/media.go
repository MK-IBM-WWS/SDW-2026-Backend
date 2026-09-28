package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"fmt"
)

const (
	fallbackImageURL = "/static/media/example.jpg"
	fallbackVideoURL = "/static/media/example.mp4"
)

func mediaURLs(t ds.CloudTariff) (string, string) {
	imageURL, videoURL := fallbackImageURL, fallbackVideoURL
	if t.ImageURL != "" {
		imageURL = fmt.Sprintf("/api/tariffs/%d/image", t.TariffID)
	}
	if t.VideoURL != "" {
		videoURL = fmt.Sprintf("/api/tariffs/%d/video", t.TariffID)
	}
	return imageURL, videoURL
}

// Missing objects redirect to static examples; browser decoding failures use media-fallback.js.
func (h *Handler) applyMediaFallbacks(t *ds.CloudTariff) {
	if t == nil {
		return
	}
	t.ImageURL, t.VideoURL = mediaURLs(*t)
}
