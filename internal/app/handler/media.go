package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
)

// Локальные заглушки используются, если имени файла нет или файл не удалось найти в MinIO.
const (
	fallbackImageURL = "/static/media/example.jpg"
	fallbackVideoURL = "/static/media/example.mp4"
)

// После удаления лишних API-маршрутов медиа получаем по прямым временным ссылкам MinIO.
func (h *Handler) mediaURLs(ctx context.Context, t ds.CloudTariff) (string, string) {
	// Эта небольшая функция одинаково обрабатывает изображение и видео: ссылка или заглушка.
	resolve := func(name, fallback string) string {
		if name != "" {
			// Ссылку выдаём, только если репозиторий нашёл файл и успешно подписал адрес скачивания.
			if url, err := h.Repository.MediaURL(ctx, name); err == nil && url != "" {
				return url
			}
		}
		return fallback
	}
	return resolve(t.ImageURL, fallbackImageURL), resolve(t.VideoURL, fallbackVideoURL)
}
