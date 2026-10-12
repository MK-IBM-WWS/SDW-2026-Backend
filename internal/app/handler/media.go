package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
)

const (
	fallbackImageURL = "/static/media/example.jpg"
	fallbackVideoURL = "/static/media/example.mp4"
)

func (h *Handler) mediaURLs(ctx context.Context, t ds.CloudTariff) (string, string) {
	resolve := func(name, fallback string) string {
		if name != "" {
			if url, err := h.Repository.MediaURL(ctx, name); err == nil && url != "" {
				return url
			}
		}
		return fallback
	}
	return resolve(t.ImageURL, fallbackImageURL), resolve(t.VideoURL, fallbackVideoURL)
}
