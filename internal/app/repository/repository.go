package repository

import (
	"database/sql"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Repository хранит оба представления подключения к одной базе данных.
// GORM используется для чтения, создания и публикации тарифов, а *sql.DB —
// только для требуемого заданием UPDATE без ORM при логическом удалении.
type Repository struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

func New(dsn string) (*Repository, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("подключение к PostgreSQL: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("получение SQL-подключения: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("проверка PostgreSQL: %w", err)
	}

	return &Repository{db: db, sqlDB: sqlDB}, nil
}

func (r *Repository) Close() error {
	return r.sqlDB.Close()
}
