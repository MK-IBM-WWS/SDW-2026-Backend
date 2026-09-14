package ds

import (
	"strings"
	"time"
)

type TariffStatus string

const (
	StatusDraft     TariffStatus = "черновик"
	StatusPublished TariffStatus = "опубликован"
	StatusDeleted   TariffStatus = "удален"
)

// CloudTariff — предметная таблица услуг облачного хостинга.
type CloudTariff struct {
	TariffID         uint         `gorm:"primaryKey"`
	TariffName       string       `gorm:"type:varchar(100);not null"`
	ShortDescription string       `gorm:"type:varchar(500);not null;default:''"`
	TariffStatus     TariffStatus `gorm:"type:varchar(20);not null;index;check:check_cloud_tariff_status,tariff_status IN ('черновик','опубликован','удален')"`
	ImageURL         string       `gorm:"type:varchar(500);not null"`
	VideoURL         string       `gorm:"type:varchar(500);not null"`
	PricePerMonth    int          `gorm:"not null;default:0;check:check_cloud_tariff_price,price_per_month >= 0"`
	RAMGB            int          `gorm:"column:ram_gb;not null;default:0;check:check_cloud_tariff_ram,ram_gb >= 0"`
	CreatedAt        time.Time    `gorm:"not null;autoCreateTime"`
	CreatorID        uint         `gorm:"not null;index"`
	FormedAt         *time.Time

	// Вычисляется SELECT-подзапросом; отдельной колонкой в таблице не является.
	LikeCount int64 `gorm:"column:like_count;->;-:migration"`
}

func (CloudTariff) TableName() string {
	return "cloud_tariffs"
}

func (t CloudTariff) DescriptionPreview() string {
	words := strings.Fields(t.ShortDescription)
	preview := ""
	for _, word := range words {
		candidate := word
		if preview != "" {
			candidate = preview + " " + word
		}
		if len([]rune(candidate)) > 20 {
			break
		}
		preview = candidate
	}
	if preview == "" && len(words) > 0 {
		word := []rune(words[0])
		if len(word) > 20 {
			word = word[:20]
		}
		preview = string(word)
	}
	return preview
}
