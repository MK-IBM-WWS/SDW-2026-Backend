package ds

import (
	"time"
)

type TariffStatus string

const (
	StatusDraft     TariffStatus = "черновик"
	StatusPublished TariffStatus = "опубликован"
	StatusDeleted   TariffStatus = "удален"
)

type CloudTariff struct {
	TariffID         uint         `gorm:"primaryKey" json:"tariff_id"`
	TariffName       string       `gorm:"type:varchar(100);not null" json:"tariff_name"`
	ShortDescription string       `gorm:"type:varchar(500);not null;default:''" json:"short_description"`
	TariffStatus     TariffStatus `gorm:"type:varchar(20);not null;index;check:check_cloud_tariff_status,tariff_status IN ('черновик','опубликован','удален')" json:"tariff_status"`
	ImageURL         string       `gorm:"type:varchar(500);not null" json:"image_name"`
	VideoURL         string       `gorm:"type:varchar(500);not null" json:"video_name"`
	PricePerMonth    int          `gorm:"not null;default:0;check:check_cloud_tariff_price,price_per_month >= 0" json:"price_per_month"`
	RAMGB            int          `gorm:"column:ram_gb;not null;default:0;check:check_cloud_tariff_ram,ram_gb >= 0" json:"ram_gb"`
	CreatedAt        time.Time    `gorm:"not null;autoCreateTime" json:"created_at"`
	CreatorID        uint         `gorm:"not null;index" json:"creator_id"`
	FormedAt         *time.Time   `json:"formed_at"`

	Creator   User  `gorm:"foreignKey:CreatorID;references:UserID" json:"-"`
	LikeCount int64 `gorm:"column:like_count;->;-:migration" json:"like_count"`
	IsOwner   int   `gorm:"-" json:"is_owner"`
	IsLiked   int   `gorm:"column:is_liked;->;-:migration" json:"is_liked"`
}

func (CloudTariff) TableName() string {
	return "cloud_tariffs"
}
