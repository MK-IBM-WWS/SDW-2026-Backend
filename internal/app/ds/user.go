package ds

// Модель пользователя: GORM связывает поля структуры со столбцами users.
// В ЛР4 PasswordHash используется для настоящего входа, а IsModerator — для роли JWT.
type User struct {
	UserID uint   `gorm:"primaryKey" json:"user_id"`
	Login  string `gorm:"type:varchar(50);uniqueIndex;not null" json:"login"`
	// json:"-" запрещает отправлять хеш пароля в ответах. swaggerignore убирает его из Swagger-схемы.
	PasswordHash string `gorm:"type:varchar(255);not null" json:"-" swaggerignore:"true"`
	// Этим флагом управляет сервер или администратор БД; регистрация всегда ставит false.
	IsModerator bool `gorm:"not null;default:false" json:"is_moderator"`
}

// Явно задаём имя таблицы users.
func (User) TableName() string {
	return "users"
}
