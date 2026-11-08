package repository

import (
	"context"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
	"gorm.io/gorm"
)

const PriceIndexName = "idx_cloud_tariffs_published_price"
const CreatePriceIndex = `CREATE INDEX IF NOT EXISTS idx_cloud_tariffs_published_price
 ON cloud_tariffs (price_per_month, tariff_id) WHERE tariff_status = 'опубликован'`

// Count и SELECT используют одинаковые условия; сортировка устойчива при одинаковой цене.
// Время включает оба SQL-запроса и загрузку авторов, но не MinIO и сериализацию HTTP.
func (r *Repository) GetPublishedTariffs(ctx context.Context, maxPrice *int, viewer uint, limit, offset int) ([]ds.CloudTariff, int64, float64, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, 0, &InputError{Message: "Некорректная пагинация"}
	}
	started := time.Now()
	filter := func(q *gorm.DB) *gorm.DB {
		q = q.Where("tariff_status = 'опубликован'")
		if maxPrice != nil {
			q = q.Where("price_per_month <= ?", *maxPrice)
		}
		return q
	}
	var total int64
	if err := filter(r.db.WithContext(ctx).Model(&ds.CloudTariff{})).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	rows := make([]ds.CloudTariff, 0)
	err := filter(r.tariffQuery(viewer).WithContext(ctx)).Order("price_per_month, tariff_id").Limit(limit).Offset(offset).Find(&rows).Error
	for i := range rows {
		if viewer > 0 && rows[i].CreatorID == viewer {
			rows[i].IsOwner = 1
		}
	}
	return rows, total, float64(time.Since(started).Microseconds()) / 1000, err
}

func (r *Repository) PriceIndexEnabled(ctx context.Context) (bool, error) {
	if r.db.Dialector.Name() != "postgres" {
		return false, nil
	}
	var exists bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname=current_schema() AND indexname=?)`, PriceIndexName).Scan(&exists).Error
	return exists, err
}

// RANDOM() перемешивает строки PostgreSQL; LIMIT ограничивает материализуемую ленту сотней.
func (r *Repository) RandomPublished(ctx context.Context, limit int) ([]ds.CloudTariff, error) {
	rows := make([]ds.CloudTariff, 0)
	err := r.db.WithContext(ctx).Where("tariff_status = ?", ds.StatusPublished).Order("RANDOM()").Limit(limit).Find(&rows).Error
	return rows, err
}
