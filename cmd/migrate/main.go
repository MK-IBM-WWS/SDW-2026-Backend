package main

import (
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm/clause"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/dsn"
	"cloud-tariffs-backend/internal/app/repository"
)

// Подготавливаем PostgreSQL: создаём нужные таблицы и ограничения, затем учебные данные для входа.
func main() {
	databaseDSN, err := dsn.FromEnv()
	if err != nil {
		log.Fatal(err)
	}

	db, err := gorm.Open(postgres.Open(databaseDSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		log.Fatalf("не удалось подключиться к БД: %v", err)
	}

	// GORM сверяет модели и структуру БД. Миграция не предназначена для удаления всех существующих данных.
	if err := db.AutoMigrate(&ds.User{}, &ds.CloudTariff{}, &ds.UserTariffLike{}); err != nil {
		log.Fatalf("не удалось выполнить миграцию: %v", err)
	}

	// Внешние ключи связывают тарифы и лайки с настоящими пользователями и тарифами.
	if err := db.Exec(`
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'fk_cloud_tariffs_creator'
			) THEN
				ALTER TABLE cloud_tariffs
					ADD CONSTRAINT fk_cloud_tariffs_creator
					FOREIGN KEY (creator_id) REFERENCES users(user_id)
					ON UPDATE RESTRICT ON DELETE RESTRICT;
			END IF;

			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'fk_user_tariff_likes_user'
			) THEN
				ALTER TABLE user_tariff_likes
					ADD CONSTRAINT fk_user_tariff_likes_user
					FOREIGN KEY (user_id) REFERENCES users(user_id)
					ON UPDATE RESTRICT ON DELETE RESTRICT;
			END IF;

			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint WHERE conname = 'fk_user_tariff_likes_tariff'
			) THEN
				ALTER TABLE user_tariff_likes
					ADD CONSTRAINT fk_user_tariff_likes_tariff
					FOREIGN KEY (tariff_id) REFERENCES cloud_tariffs(tariff_id)
					ON UPDATE RESTRICT ON DELETE RESTRICT;
			END IF;
		END $$;
	`).Error; err != nil {
		log.Fatalf("не удалось создать внешние ключи: %v", err)
	}

	// Индекс действует только для статуса черновик: у одного владельца может быть максимум один такой тариф.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_cloud_tariffs_one_draft_per_creator
		ON cloud_tariffs (creator_id)
		WHERE tariff_status = 'черновик'
	`).Error; err != nil {
		log.Fatalf("не удалось создать индекс одного черновика: %v", err)
	}

	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_likes_tariff ON user_tariff_likes(tariff_id)").Error; err != nil {
		log.Fatal(err)
	}

	// ЛР4. Создаём bcrypt-хеш учебного пароля student123, чтобы учебный аккаунт мог реально войти.
	hash, err := bcrypt.GenerateFromPassword([]byte("student123"), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	user := ds.User{UserID: 1, Login: "student", PasswordHash: string(hash)}
	// Если пользователь с ID=1 уже есть, DoNothing сохраняет его данные, включая настоящий пароль.
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoNothing: true}).Create(&user).Error; err != nil {
		log.Fatal(err)
	}
	// Repair only the known lab-2 seed placeholder, preserving real account passwords.
	// Исправляем только известный текст-заглушку старой лабораторной.
	// Настоящие пароли существующих аккаунтов это условие не затрагивает.
	for login, password := range map[string]string{"student": "student123", "moderator": "moderator123", "guest": "guest123"} {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal(err)
		}
		if err := db.Model(&ds.User{}).Where("login = ? AND password_hash = ?", login, "lab2-password-not-for-production").Update("password_hash", string(hash)).Error; err != nil {
			log.Fatal(err)
		}
	}
	// Convert the old lab's full MinIO URLs into object names without touching files.
	// Переход от старых полных адресов MinIO к хранению имён объектов. Сами файлы не перемещаем.
	if err := db.Exec(`UPDATE cloud_tariffs SET
        image_url = CASE WHEN image_url ~ '^https?://' THEN regexp_replace(image_url, '^.*/', '') ELSE image_url END,
        video_url = CASE WHEN video_url ~ '^https?://' THEN regexp_replace(video_url, '^.*/', '') ELSE video_url END
        WHERE image_url ~ '^https?://' OR video_url ~ '^https?://'`).Error; err != nil {
		log.Fatal(err)
	}
	// Согласуем следующий автоматически выдаваемый ID пользователя с уже существующими строками.
	if err := db.Exec(`SELECT setval(pg_get_serial_sequence('users','user_id'), GREATEST((SELECT COALESCE(MAX(user_id),1) FROM users), (SELECT last_value FROM users_user_id_seq)), true)`).Error; err != nil {
		log.Fatal(err)
	}

	if err := db.Exec(repository.CreatePriceIndex).Error; err != nil {
		log.Fatal(err)
	}

	log.Println("Миграция выполнена: users, cloud_tariffs, user_tariff_likes")
}
