package main

import (
	"cloud-tariffs-backend/internal/app/currentuser"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm/clause"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/dsn"
)

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

	if err := db.AutoMigrate(&ds.User{}, &ds.CloudTariff{}, &ds.UserTariffLike{}); err != nil {
		log.Fatalf("не удалось выполнить миграцию: %v", err)
	}

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

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_cloud_tariffs_one_draft_per_creator
		ON cloud_tariffs (creator_id)
		WHERE tariff_status = 'черновик'
	`).Error; err != nil {
		log.Fatalf("не удалось создать индекс одного черновика: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("student123"), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	user := ds.User{UserID: currentuser.CreatorID, Login: "student", PasswordHash: string(hash)}
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoNothing: true}).Create(&user).Error; err != nil {
		log.Fatal(err)
	}
	// Convert the old lab's full MinIO URLs into object names without touching files.
	if err := db.Exec(`UPDATE cloud_tariffs SET
        image_url = CASE WHEN image_url ~ '^https?://' THEN regexp_replace(image_url, '^.*/', '') ELSE image_url END,
        video_url = CASE WHEN video_url ~ '^https?://' THEN regexp_replace(video_url, '^.*/', '') ELSE video_url END`).Error; err != nil {
		log.Fatal(err)
	}
	if err := db.Exec(`SELECT setval(pg_get_serial_sequence('users','user_id'), GREATEST((SELECT COALESCE(MAX(user_id),1) FROM users), (SELECT last_value FROM users_user_id_seq)), true)`).Error; err != nil {
		log.Fatal(err)
	}

	log.Println("Миграция выполнена: users, cloud_tariffs, user_tariff_likes")
}
