package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"cloud-tariffs-backend/internal/app/ds"
)

var (
	ErrTariffNotFound = errors.New("тариф не найден")
	ErrDraftExists    = errors.New("у пользователя уже есть черновик тарифа")
)

// GetPublishedTariffs получает опубликованные тарифы и число лайков из БД через ORM.
func (r *Repository) GetPublishedTariffs(maxPrice *int) ([]ds.CloudTariff, error) {
	var tariffs []ds.CloudTariff

	query := r.db.Model(&ds.CloudTariff{}).
		Select(`cloud_tariffs.*,
			(SELECT COUNT(*) FROM user_tariff_likes
			 WHERE user_tariff_likes.tariff_id = cloud_tariffs.tariff_id) AS like_count`).
		Where("cloud_tariffs.tariff_status = ?", ds.StatusPublished)

	if maxPrice != nil {
		query = query.Where("cloud_tariffs.price_per_month <= ?", *maxPrice)
	}

	if err := query.Order("cloud_tariffs.tariff_id").Find(&tariffs).Error; err != nil {
		return nil, fmt.Errorf("получение опубликованных тарифов: %w", err)
	}
	return tariffs, nil
}

// GetPublishedTariff получает одну опубликованную и не удалённую услугу через ORM.
func (r *Repository) GetPublishedTariff(tariffID uint) (*ds.CloudTariff, error) {
	var tariff ds.CloudTariff
	err := r.db.Model(&ds.CloudTariff{}).
		Select(`cloud_tariffs.*,
			(SELECT COUNT(*) FROM user_tariff_likes
			 WHERE user_tariff_likes.tariff_id = cloud_tariffs.tariff_id) AS like_count`).
		Where("cloud_tariffs.tariff_id = ? AND cloud_tariffs.tariff_status = ?", tariffID, ds.StatusPublished).
		First(&tariff).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTariffNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("получение тарифа: %w", err)
	}
	return &tariff, nil
}

func (r *Repository) GetFirstPublishedTariff() (*ds.CloudTariff, error) {
	tariffs, err := r.GetPublishedTariffs(nil)
	if err != nil {
		return nil, err
	}
	if len(tariffs) == 0 {
		return nil, ErrTariffNotFound
	}
	return &tariffs[0], nil
}

func (r *Repository) GetNextPublishedTariff(afterTariffID uint) (*ds.CloudTariff, error) {
	var tariff ds.CloudTariff
	err := r.db.Model(&ds.CloudTariff{}).
		Select(`cloud_tariffs.*,
			(SELECT COUNT(*) FROM user_tariff_likes
			 WHERE user_tariff_likes.tariff_id = cloud_tariffs.tariff_id) AS like_count`).
		Where("cloud_tariffs.tariff_status = ? AND cloud_tariffs.tariff_id > ?", ds.StatusPublished, afterTariffID).
		Order("cloud_tariffs.tariff_id").
		First(&tariff).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.GetFirstPublishedTariff()
	}
	if err != nil {
		return nil, fmt.Errorf("получение следующего тарифа: %w", err)
	}
	return &tariff, nil
}

// GetDraftTariff возвращает только черновик конкретного пользователя.
// Отсутствие черновика — нормальное состояние, поэтому возвращается nil без ошибки.
func (r *Repository) GetDraftTariff(creatorID uint) (*ds.CloudTariff, error) {
	var tariff ds.CloudTariff
	err := r.db.Where("creator_id = ? AND tariff_status = ?", creatorID, ds.StatusDraft).
		First(&tariff).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("получение черновика: %w", err)
	}
	return &tariff, nil
}

// CreateDraftTariff создаёт черновик через ORM только после нажатия «Далее».
func (r *Repository) CreateDraftTariff(tariff *ds.CloudTariff) error {
	var count int64
	if err := r.db.Model(&ds.CloudTariff{}).
		Where("creator_id = ? AND tariff_status = ?", tariff.CreatorID, ds.StatusDraft).
		Count(&count).Error; err != nil {
		return fmt.Errorf("проверка существования черновика: %w", err)
	}
	if count > 0 {
		return ErrDraftExists
	}

	if err := r.db.Create(tariff).Error; err != nil {
		return fmt.Errorf("создание черновика: %w", err)
	}
	return nil
}

type PublishTariffInput struct {
	TariffID         uint
	CreatorID        uint
	TariffName       string
	ImageURL         string
	VideoURL         string
	ShortDescription string
	PricePerMonth    int
	RAMGB            int
}

// PublishDraftTariff заполняет предметные поля и меняет статус через ORM.
func (r *Repository) PublishDraftTariff(input PublishTariffInput) error {
	formedAt := time.Now()
	result := r.db.Model(&ds.CloudTariff{}).
		Where("tariff_id = ? AND creator_id = ? AND tariff_status = ?", input.TariffID, input.CreatorID, ds.StatusDraft).
		Updates(map[string]any{
			"tariff_name":       input.TariffName,
			"image_url":         input.ImageURL,
			"video_url":         input.VideoURL,
			"short_description": input.ShortDescription,
			"price_per_month":   input.PricePerMonth,
			"ram_gb":            input.RAMGB,
			"tariff_status":     ds.StatusPublished,
			"formed_at":         formedAt,
		})
	if result.Error != nil {
		return fmt.Errorf("публикация тарифа: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrTariffNotFound
	}
	return nil
}

// DeleteTariff выполняет параметризованный SQL UPDATE через database/sql, без ORM.
func (r *Repository) DeleteTariff(ctx context.Context, tariffID uint) error {
	result, err := r.sqlDB.ExecContext(ctx, `
		UPDATE cloud_tariffs
		SET tariff_status = $1
		WHERE tariff_id = $2 AND tariff_status = $3`, ds.StatusDeleted, tariffID, ds.StatusPublished)
	if err != nil {
		return fmt.Errorf("логическое удаление тарифа %d: %w", tariffID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("получение результата удаления: %w", err)
	}
	if rowsAffected == 0 {
		return ErrTariffNotFound
	}
	return nil
}
