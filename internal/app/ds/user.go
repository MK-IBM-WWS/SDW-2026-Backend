package ds

type User struct {
	UserID       uint   `gorm:"primaryKey" json:"user_id"`
	Login        string `gorm:"type:varchar(50);uniqueIndex;not null" json:"login"`
	PasswordHash string `gorm:"type:varchar(255);not null" json:"-"`
	IsModerator  bool   `gorm:"not null;default:false" json:"is_moderator"`
}

func (User) TableName() string {
	return "users"
}
