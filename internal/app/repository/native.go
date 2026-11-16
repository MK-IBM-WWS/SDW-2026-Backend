package repository

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
	"io"
	"strings"
)

// NativeCatalog adds guest name search and an optional CLIP seed selection.
// Values are bound parameters; user input never becomes SQL syntax.
func (r *Repository) NativeCatalog(ctx context.Context, q string, maxPrice *int, demo bool, limit, offset int) ([]ds.CloudTariff, int64, error) {
	return r.nativeCatalog(ctx, q, maxPrice, demo, false, limit, offset)
}

// Semantic candidates are selected by description format, never by test-record names.
func (r *Repository) NativeSearchCatalog(ctx context.Context, q string, limit, offset int) ([]ds.CloudTariff, int64, error) {
	return r.nativeCatalog(ctx, q, nil, false, true, limit, offset)
}
func (r *Repository) nativeCatalog(ctx context.Context, q string, maxPrice *int, demo, semantic bool, limit, offset int) ([]ds.CloudTariff, int64, error) {
	filter := func(db *gorm.DB) *gorm.DB {
		db = db.Where("tariff_status = ?", ds.StatusPublished)
		if q != "" {
			db = db.Where("LOWER(tariff_name || ' ' || short_description) LIKE ? ESCAPE '!'", "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(q))+"%")
		}
		if maxPrice != nil {
			db = db.Where("price_per_month <= ?", *maxPrice)
		}
		if demo {
			db = db.Where("tariff_name LIKE ?", "CLIP-%")
		}
		if semantic {
			if r.db.Dialector.Name() == "sqlite" {
				db = db.Where("length(short_description) BETWEEN 50 AND 100 AND short_description NOT GLOB '*[^ -~]*'")
			} else {
				db = db.Where("short_description ~ ?", "^[ -~]{50,100}$")
			}
		}
		return db
	}
	var total int64
	if err := filter(r.db.WithContext(ctx).Model(&ds.CloudTariff{})).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]ds.CloudTariff, 0)
	err := filter(r.tariffQuery(0).WithContext(ctx)).Order("tariff_id").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, total, err
}

// Image bytes are served by the API, so LAN clients never receive localhost MinIO URLs.
func (r *Repository) OpenNativeImage(ctx context.Context, name string) (io.ReadCloser, string, error) {
	stat, err := r.minio.StatObject(ctx, r.bucket, name, minio.StatObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	obj, err := r.minio.GetObject(ctx, r.bucket, name, minio.GetObjectOptions{})
	return obj, stat.ContentType, err
}
