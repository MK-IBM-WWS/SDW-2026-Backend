package repository

import (
	"context"
	"errors"
	"time"

	"cloud-tariffs-backend/internal/app/currentuser"
	"cloud-tariffs-backend/internal/app/ds"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTariffNotFound = errors.New("тариф не найден")
	ErrDraftExists    = errors.New("у пользователя уже есть черновик")
	ErrForbidden      = errors.New("можно изменять только свои тарифы")
	ErrInvalidState   = errors.New("операция недоступна в текущем статусе")
	ErrLoginExists    = errors.New("логин уже занят")
)

func (r *Repository) tariffQuery() *gorm.DB {
	return r.db.Model(&ds.CloudTariff{}).Preload("Creator").Select(`cloud_tariffs.*,
        (SELECT COUNT(*) FROM user_tariff_likes l WHERE l.tariff_id=cloud_tariffs.tariff_id) AS like_count,
        CASE WHEN EXISTS (SELECT 1 FROM user_tariff_likes l WHERE l.tariff_id=cloud_tariffs.tariff_id AND l.user_id=?) THEN 1 ELSE 0 END AS is_liked`, currentuser.Get().ID())
}
func (r *Repository) GetPublishedTariffs(maxPrice *int) ([]ds.CloudTariff, error) {
	tariffs := make([]ds.CloudTariff, 0)
	q := r.tariffQuery().Where("tariff_status = ?", ds.StatusPublished)
	if maxPrice != nil {
		q = q.Where("price_per_month <= ?", *maxPrice)
	}
	err := q.Order("tariff_id").Find(&tariffs).Error
	for i := range tariffs {
		if tariffs[i].CreatorID == currentuser.Get().ID() {
			tariffs[i].IsOwner = 1
		}
	}
	return tariffs, err
}
func (r *Repository) GetPublishedTariff(id uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := r.tariffQuery().Where("tariff_id = ? AND tariff_status = ?", id, ds.StatusPublished).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrTariffNotFound
	}
	return &t, err
}
func (r *Repository) GetFirstPublishedTariff() (*ds.CloudTariff, error) {
	return r.GetNextPublishedTariff(0)
}
func (r *Repository) GetNextPublishedTariff(after uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := r.tariffQuery().Where("tariff_status = ? AND tariff_id > ?", ds.StatusPublished, after).Order("tariff_id").First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && after > 0 {
		return r.GetNextPublishedTariff(0)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrTariffNotFound
	}
	return &t, err
}
func (r *Repository) GetDraftTariff(creator uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := r.tariffQuery().Where("creator_id = ? AND tariff_status = ?", creator, ds.StatusDraft).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &t, err
}
func (r *Repository) CreateDraftTariff(t *ds.CloudTariff) error {
	// The partial unique index also protects against simultaneous HTTP requests.
	err := r.db.Omit("Creator").Create(t).Error
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "idx_cloud_tariffs_one_draft_per_creator" {
		return ErrDraftExists
	}
	return err
}

type PublishTariffInput struct {
	TariffID         uint
	CreatorID        uint
	TariffName       string
	ShortDescription string
	PricePerMonth    int
	RAMGB            int
}

func ownedActive(tx *gorm.DB, id, creator uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tariff_id = ? AND tariff_status <> ?", id, ds.StatusDeleted).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTariffNotFound
	}
	if err != nil {
		return nil, err
	}
	if t.CreatorID != creator {
		return nil, ErrForbidden
	}
	return &t, nil
}
func (r *Repository) PublishDraftTariff(in PublishTariffInput) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		t, err := ownedActive(tx, in.TariffID, in.CreatorID)
		if err != nil {
			return err
		}
		if t.TariffStatus != ds.StatusDraft {
			return ErrInvalidState
		}
		return tx.Model(t).Updates(map[string]any{
			"tariff_name": in.TariffName, "short_description": in.ShortDescription,
			"price_per_month": in.PricePerMonth, "ram_gb": in.RAMGB,
			"tariff_status": ds.StatusPublished, "formed_at": time.Now().UTC(),
		}).Error
	})
}
func (r *Repository) DeleteTariff(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := ownedActive(tx, id, currentuser.Get().ID())
		if err != nil {
			return err
		}
		// Soft delete: preserve the row and its files and likes.
		return tx.Model(t).Update("tariff_status", ds.StatusDeleted).Error
	})
}
func (r *Repository) SetLike(ctx context.Context, id uint, value int) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var t ds.CloudTariff
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tariff_id = ? AND tariff_status = ?", id, ds.StatusPublished).First(&t).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTariffNotFound
		}
		if err != nil {
			return err
		}
		like := ds.UserTariffLike{UserID: currentuser.Get().ID(), TariffID: id}
		if value == 1 {
			err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "tariff_id"}}, DoNothing: true}).Create(&like).Error
		} else {
			err = tx.Where("user_id = ? AND tariff_id = ?", like.UserID, id).Delete(&ds.UserTariffLike{}).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&ds.UserTariffLike{}).Where("tariff_id = ?", id).Count(&count).Error
	})
	return count, err
}
func (r *Repository) Register(u *ds.User) error {
	err := r.db.Create(u).Error
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrLoginExists
	}
	return err
}
func (r *Repository) UserTariffs(id uint) (*ds.User, []ds.CloudTariff, error) {
	var u ds.User
	err := r.db.First(&u, id).Error
	if err != nil {
		return nil, nil, err
	}
	list := make([]ds.CloudTariff, 0)
	err = r.tariffQuery().Where("creator_id = ? AND tariff_status = ?", id, ds.StatusPublished).Order("tariff_id").Find(&list).Error
	return &u, list, err
}

func (r *Repository) GetUser(id uint) (ds.User, error) {
	var u ds.User
	err := r.db.First(&u, id).Error
	return u, err
}
