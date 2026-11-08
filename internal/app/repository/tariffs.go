package repository

import (
	"context"
	"errors"
	"time"

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

// Считаем общее число лайков и отдельный признак лайка именно для viewer из JWT.
func (r *Repository) tariffQuery(viewer uint) *gorm.DB {
	return r.db.Model(&ds.CloudTariff{}).Preload("Creator").Select(`cloud_tariffs.*,
        (SELECT COUNT(*) FROM user_tariff_likes l WHERE l.tariff_id=cloud_tariffs.tariff_id) AS like_count,
        CASE WHEN EXISTS (SELECT 1 FROM user_tariff_likes l WHERE l.tariff_id=cloud_tariffs.tariff_id AND l.user_id=?) THEN 1 ELSE 0 END AS is_liked`, viewer)
}

// Ищем одну опубликованную карточку. Внешнего GET по этому пути нет: метод нужен обработчику ленты и публикации.
func (r *Repository) GetPublishedTariff(id, viewer uint) (*ds.CloudTariff, error) {
	return r.PublishedCard(context.Background(), id, viewer)
}

// PublishedCard проверяет актуальный статус и лайки с таймаутом запроса ленты.
func (r *Repository) PublishedCard(ctx context.Context, id, viewer uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := r.tariffQuery(viewer).WithContext(ctx).Where("tariff_id = ? AND tariff_status = ?", id, ds.StatusPublished).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrTariffNotFound
	}
	return &t, err
}

// Находим черновик только указанного владельца. Его ID приходит из JWT, а не из параметра клиента.
func (r *Repository) GetDraftTariff(creator uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := r.tariffQuery(creator).Where("creator_id = ? AND tariff_status = ?", creator, ds.StatusDraft).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &t, err
}

// Сохраняем новый черновик. Уникальный индекс БД запрещает два черновика одного владельца даже при одновременных запросах.
func (r *Repository) CreateDraftTariff(t *ds.CloudTariff) error {
	// The partial unique index also protects against simultaneous HTTP requests.
	err := r.db.Omit("Creator").Create(t).Error
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "idx_cloud_tariffs_one_draft_per_creator" {
		return ErrDraftExists
	}
	return err
}

// Поля публикации. CreatorID подставлен сервером; остальные редактируемые поля прочитаны из JSON.
type PublishTariffInput struct {
	TariffID         uint
	CreatorID        uint
	TariffName       string
	ShortDescription string
	PricePerMonth    int
	RAMGB            int
	Context          context.Context
	Image, Video     *Upload
}

// Проверка авторизации для конкретного тарифа: запись существует, не удалена и принадлежит этому пользователю.
// Блокировка строки на время транзакции защищает её от одновременных изменений.
func ownedActive(tx *gorm.DB, id, creator uint) (*ds.CloudTariff, error) {
	var t ds.CloudTariff
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tariff_id = ? AND tariff_status <> ?", id, ds.StatusDeleted).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTariffNotFound
	}
	if err != nil {
		return nil, err
	}
	// Это проверка владельца. Токен может быть верным, но чужой тариф всё равно менять нельзя.
	if t.CreatorID != creator {
		return nil, ErrForbidden
	}
	return &t, nil
}

// Транзакция объединяет проверку владельца и обновление полей в одну операцию с данными.
// Если одна часть не удалась, изменения транзакции отменяются.
func (r *Repository) PublishDraftTariff(in PublishTariffInput) error {
	ctx := in.Context
	if ctx == nil {
		ctx = context.Background()
	}
	var image, video, oldImage, oldVideo string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := ownedActive(tx, in.TariffID, in.CreatorID)
		if err != nil {
			return err
		}
		if t.TariffStatus != ds.StatusDraft {
			return ErrInvalidState
		}
		// Загружаем только выбранные новые файлы после проверки владельца.
		image, err = r.upload(ctx, in.Image, "image")
		if err != nil {
			return err
		}
		video, err = r.upload(ctx, in.Video, "video")
		if err != nil {
			return err
		}
		fields := map[string]any{
			"tariff_name": in.TariffName, "short_description": in.ShortDescription,
			"price_per_month": in.PricePerMonth, "ram_gb": in.RAMGB,
			"tariff_status": ds.StatusPublished, "formed_at": time.Now().UTC(),
		}
		if image != "" {
			oldImage = t.ImageURL
			fields["image_url"] = image
		}
		if video != "" {
			oldVideo = t.VideoURL
			fields["video_url"] = video
		}
		// UPDATE существующей строки: ID тарифа и автор остаются прежними.
		return tx.Model(t).Updates(fields).Error
	})
	if err != nil {
		// При сбое загрузки/БД старые поля остаются, новые объекты убираем.
		if image != "" || video != "" {
			r.cleanup(image, video)
		}
		return err
	}
	// Общие демонстрационные объекты не удаляем, пока на них есть ссылки.
	for _, name := range []string{oldImage, oldVideo} {
		if name == "" {
			continue
		}
		var count int64
		if err := r.db.Model(&ds.CloudTariff{}).Where("image_url = ? OR video_url = ?", name, name).Count(&count).Error; err == nil && count == 0 {
			r.cleanup(name)
		}
	}
	return nil
}

// Soft delete: меняем статус, но сохраняем запись, файлы и лайки. Сначала проверяем владельца.
func (r *Repository) DeleteTariff(ctx context.Context, id, viewer uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t, err := ownedActive(tx, id, viewer)
		if err != nil {
			return err
		}
		// Soft delete: preserve the row and its files and likes.
		return tx.Model(t).Update("tariff_status", ds.StatusDeleted).Error
	})
}

// Связываем ID из JWT и ID опубликованного тарифа. Один пользователь не получает несколько одинаковых лайков.
func (r *Repository) SetLike(ctx context.Context, id, viewer uint, value int) (int64, error) {
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
		like := ds.UserTariffLike{UserID: viewer, TariffID: id}
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

// Добавляем аккаунт. Код PostgreSQL 23505 означает нарушение уникальности, здесь — занятый логин.
func (r *Repository) Register(u *ds.User) error {
	err := r.db.Create(u).Error
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrLoginExists
	}
	return err
}

// Получаем сведения пользователя для формирования ответа о созданном тарифе.
func (r *Repository) GetUser(id uint) (ds.User, error) {
	var u ds.User
	err := r.db.First(&u, id).Error
	return u, err
}

// ЛР4. Ищем аккаунт при входе по нормализованному логину. Сам пароль проверяется выше через bcrypt.
func (r *Repository) GetUserByLogin(login string) (ds.User, error) {
	var u ds.User
	err := r.db.Where("login = ?", login).First(&u).Error
	return u, err
}
