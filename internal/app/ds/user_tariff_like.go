package ds

type UserTariffLike struct {
	LikeID   uint `gorm:"primaryKey"`
	UserID   uint `gorm:"not null;uniqueIndex:idx_user_tariff_like"`
	TariffID uint `gorm:"not null;uniqueIndex:idx_user_tariff_like"`
}

func (UserTariffLike) TableName() string {
	return "user_tariff_likes"
}
