package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
	"github.com/minio/minio-go/v7"
)

const MaxImageSize int64 = 10 << 20
const MaxVideoSize int64 = 50 << 20

type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }
func Invalid(s string) error        { return &InputError{Message: s} }

type Upload struct {
	Header          *multipart.FileHeader
	Type, Extension string
}

// Inspect actual bytes, not the user-controlled extension or Content-Type.
func ValidateUpload(_ context.Context, h *multipart.FileHeader, video bool) (*Upload, error) {
	if h == nil {
		return nil, nil
	}
	limit := MaxImageSize
	if video {
		limit = MaxVideoSize
	}
	if h.Size <= 0 || h.Size > limit {
		return nil, Invalid("файл пустой или превышает допустимый размер")
	}
	f, err := h.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}
	typ := http.DetectContentType(buf[:n])
	types := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}
	if video {
		types = map[string]string{"video/mp4": ".mp4", "video/webm": ".webm"}
	}
	ext, ok := types[typ]
	if !ok {
		return nil, Invalid("неподдерживаемый тип файла: " + typ)
	}
	return &Upload{Header: h, Type: typ, Extension: ext}, nil
}
func (r *Repository) upload(ctx context.Context, u *Upload, prefix string) (string, error) {
	if u == nil {
		return "", nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	name := prefix + "_" + hex.EncodeToString(b) + u.Extension
	f, err := u.Header.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = r.minio.PutObject(ctx, r.bucket, name, f, u.Header.Size, minio.PutObjectOptions{ContentType: u.Type})
	if err != nil {
		return "", fmt.Errorf("MinIO upload: %w", err)
	}
	return name, nil
}
func (r *Repository) cleanup(names ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, name := range names {
		if name != "" {
			if err := r.minio.RemoveObject(ctx, r.bucket, name, minio.RemoveObjectOptions{}); err != nil {
				log.Printf("media cleanup %s: %v", name, err)
			}
		}
	}
}
func (r *Repository) CreateWithFiles(ctx context.Context, t *ds.CloudTariff, img, vid *Upload) error {
	existing, err := r.GetDraftTariff(t.CreatorID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrDraftExists
	}
	t.ImageURL, err = r.upload(ctx, img, "image")
	if err != nil {
		return err
	}
	t.VideoURL, err = r.upload(ctx, vid, "video")
	if err != nil {
		r.cleanup(t.ImageURL)
		return err
	}
	if err = r.CreateDraftTariff(t); err != nil {
		r.cleanup(t.ImageURL, t.VideoURL)
		return err
	}
	return nil
}

// Fetch media only through the configured MinIO client; never fetch arbitrary stored URLs.
func (r *Repository) OpenMedia(ctx context.Context, name string) (*minio.Object, minio.ObjectInfo, error) {
	info, err := r.minio.StatObject(ctx, r.bucket, name, minio.StatObjectOptions{})
	if err != nil {
		return nil, info, err
	}
	obj, err := r.minio.GetObject(ctx, r.bucket, name, minio.GetObjectOptions{})
	return obj, info, err
}
func (r *Repository) VisibleTariff(id, user uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := r.db.Where("tariff_id = ? AND (tariff_status = ? OR (tariff_status = ? AND creator_id = ?))", id, ds.StatusPublished, ds.StatusDraft, user).First(&t).Error
	return &t, err
}
