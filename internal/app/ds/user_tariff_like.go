package ds

import "time"

// UserTariffLike реализует связь многие-ко-многим «пользователи — тарифы».
type UserTariffLike struct {
	LikeID    uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"not null;uniqueIndex:idx_user_tariff_like"`
	TariffID  uint      `gorm:"not null;uniqueIndex:idx_user_tariff_like"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime"`
}

func (UserTariffLike) TableName() string {
	return "user_tariff_likes"
}
