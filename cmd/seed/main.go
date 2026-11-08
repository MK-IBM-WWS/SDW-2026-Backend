// Повторный запуск доводит учебный набор до -count строк, не трогая реальные тарифы.
package main

import (
	"flag"
	"fmt"
	"log"

	"cloud-tariffs-backend/internal/app/dsn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	count := flag.Int("count", 120000, "Число учебных тарифов (>100000)")
	flag.Parse()
	if *count <= 100000 || *count > 2000000 {
		log.Fatal("count должен быть от 100001 до 2000000")
	}
	conn, err := dsn.FromEnv()
	if err != nil {
		log.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(conn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var added int64
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(20261108)").Error; err != nil {
			return err
		}
		var owner uint
		if err := tx.Raw("SELECT user_id FROM users WHERE login='student'").Scan(&owner).Error; err != nil {
			return err
		}
		if owner == 0 {
			return fmt.Errorf("сначала выполните go run ./cmd/migrate")
		}
		// Все генерируемые строки опубликованы; черновики существующих авторов не затрагиваются.
		// Цена равномерна в диапазоне 100..500000: фильтр <=1000 даёт селективную выборку.
		result := tx.Exec(`INSERT INTO cloud_tariffs
   (tariff_name, short_description, tariff_status, image_url, video_url, price_per_month, ram_gb, creator_id, created_at, formed_at)
   SELECT 'Lab-PAG-' || lpad(n::text,7,'0'),
    'Облачный сервер: учебный тариф для проверки индексов и пагинации.',
    'опубликован','','',100+floor(random()*499901)::int,
    (ARRAY[1,2,4,8,16,32,64,128])[1+floor(random()*8)::int],?,
    now()-random()*interval '365 days',now()
   FROM generate_series(1,?) n
   WHERE NOT EXISTS (SELECT 1 FROM cloud_tariffs t WHERE t.tariff_name='Lab-PAG-' || lpad(n::text,7,'0'))`, owner, *count)
		added = result.RowsAffected
		return result.Error
	})
	if err != nil {
		log.Fatal(err)
	}
	if err = db.Exec("ANALYZE cloud_tariffs").Error; err != nil {
		log.Fatal(err)
	}
	var total int64
	db.Table("cloud_tariffs").Count(&total)
	log.Printf("Добавлено: %d; всего тарифов: %d", added, total)
}
