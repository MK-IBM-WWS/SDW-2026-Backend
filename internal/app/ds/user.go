package ds

import "time"

type User struct {
	UserID       uint      `gorm:"primaryKey"`
	Login        string    `gorm:"type:varchar(50);uniqueIndex;not null"`
	PasswordHash string    `gorm:"type:varchar(255);not null"`
	IsModerator  bool      `gorm:"not null;default:false"`
	CreatedAt    time.Time `gorm:"not null;autoCreateTime"`
}

func (User) TableName() string {
	return "users"
}
