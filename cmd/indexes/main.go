// Управление только учебным индексом. Ограничения PK/unique не отключаются.
package main

import (
	"cloud-tariffs-backend/internal/app/dsn"
	"cloud-tariffs-backend/internal/app/repository"
	"encoding/json"
	"flag"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
)

func main() {
	action := flag.String("action", "status", "enable | disable | status | explain")
	price := flag.Int("price", 1000, "Максимальная цена для EXPLAIN")
	page := flag.Int("page", 1, "Страница EXPLAIN")
	limit := flag.Int("limit", 10, "Размер страницы EXPLAIN (1..100)")
	flag.Parse()
	if *price < 0 || *page < 1 || *limit < 1 || *limit > 100 || int64(*page-1)*int64(*limit) > 2147483647 {
		log.Fatal("некорректные параметры")
	}
	conn, e := dsn.FromEnv()
	if e != nil {
		log.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(conn), &gorm.Config{})
	if e != nil {
		log.Fatal(e)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	switch *action {
	case "enable":
		e = db.Exec(repository.CreatePriceIndex).Error
	case "disable":
		e = db.Exec("DROP INDEX IF EXISTS " + repository.PriceIndexName).Error
	case "status":
		var rows []map[string]interface{}
		e = db.Raw("SELECT indexname,indexdef FROM pg_indexes WHERE tablename='cloud_tariffs' ORDER BY indexname").Scan(&rows).Error
		b, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(b))
	case "explain":
		// Те же WHERE/ORDER/LIMIT, что в React; отдельно показываем COUNT.
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`EXPLAIN (ANALYZE, BUFFERS) SELECT COUNT(*) FROM cloud_tariffs WHERE tariff_status='опубликован' AND price_per_month <= ?`, []any{*price}},
			{`EXPLAIN (ANALYZE, BUFFERS) SELECT cloud_tariffs.*,
    (SELECT COUNT(*) FROM user_tariff_likes l WHERE l.tariff_id=cloud_tariffs.tariff_id) AS like_count,
    CASE WHEN EXISTS (SELECT 1 FROM user_tariff_likes l WHERE l.tariff_id=cloud_tariffs.tariff_id AND l.user_id=0) THEN 1 ELSE 0 END AS is_liked
    FROM cloud_tariffs WHERE tariff_status='опубликован' AND price_per_month <= ?
    ORDER BY price_per_month,tariff_id LIMIT ? OFFSET ?`, []any{*price, *limit, (*page - 1) * (*limit)}},
		} {
			rows, err := db.Raw(q.sql, q.args...).Rows()
			if err != nil {
				log.Fatal(err)
			}
			for rows.Next() {
				var line string
				if err = rows.Scan(&line); err != nil {
					log.Fatal(err)
				}
				fmt.Println(line)
			}
			if err = rows.Err(); err != nil {
				log.Fatal(err)
			}
			rows.Close()
		}
	default:
		log.Fatal("action: enable | disable | status | explain")
	}
	if e != nil {
		log.Fatal(e)
	}
	if *action == "enable" || *action == "disable" {
		if e = db.Exec("ANALYZE cloud_tariffs").Error; e != nil {
			log.Fatal(e)
		}
		fmt.Println("Учебный индекс:", *action)
	}
}
