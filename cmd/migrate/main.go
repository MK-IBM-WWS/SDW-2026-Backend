package main

import (
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

	log.Println("Миграция выполнена: users, cloud_tariffs, user_tariff_likes")
}
