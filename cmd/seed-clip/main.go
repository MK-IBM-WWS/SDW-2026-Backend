// Add 100 published cloud AI tariffs, preserving every existing record.
package main

import (
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/dsn"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed records.json
var recordsJSON []byte

type record struct {
	Name        string `json:"tariff_name"`
	Description string `json:"short_description"`
	Price       int    `json:"price_per_month"`
	RAM         int    `json:"ram_gb"`
	Category    string `json:"category"`
	ImageURL    string `json:"image_url"`
}

func records() ([]record, error) {
	var rows []record
	if err := json.Unmarshal(recordsJSON, &rows); err != nil {
		return nil, err
	}
	if len(rows) != 100 {
		return nil, fmt.Errorf("expected 100 records")
	}
	names := map[string]bool{}
	for _, r := range rows {
		if names[r.Name] {
			return nil, fmt.Errorf("duplicate %s", r.Name)
		}
		names[r.Name] = true
		if n := utf8.RuneCountInString(r.Description); n < 50 || n > 100 {
			return nil, fmt.Errorf("description length: %s", r.Name)
		}
		for _, c := range r.Description {
			if c > 127 {
				return nil, fmt.Errorf("English ASCII description required: %s", r.Name)
			}
		}
	}
	return rows, nil
}
func download(client *http.Client, url, path string) error {
	if b, e := os.ReadFile(path); e == nil && len(b) > 1000 {
		return nil
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return err
	}
	if !strings.HasPrefix(http.DetectContentType(content), "image/") {
		return fmt.Errorf("not an image: %s", url)
	}
	return os.WriteFile(path, content, 0644)
}
func main() {
	rows, err := records()
	if err != nil {
		log.Fatal(err)
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
	var owner uint
	db.Raw("SELECT user_id FROM users WHERE login='student'").Scan(&owner)
	if owner == 0 {
		log.Fatal("Run go run ./cmd/migrate first")
	}
	mc, err := minio.New(os.Getenv("MINIO_ENDPOINT"), &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"), ""), Secure: os.Getenv("MINIO_USE_SSL") == "true"})
	if err != nil {
		log.Fatal(err)
	}
	bucket := os.Getenv("MINIO_BUCKET_NAME")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	exists, err := mc.BucketExists(ctx, bucket)
	if err != nil {
		log.Fatal(err)
	}
	if !exists {
		if err = mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatal(err)
		}
	}
	if err = os.MkdirAll("test-images", 0755); err != nil {
		log.Fatal(err)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.Category] {
			continue
		}
		seen[r.Category] = true
		path := filepath.Join("test-images", r.Category+".jpg")
		if err = download(client, r.ImageURL, path); err != nil {
			log.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		typ := http.DetectContentType(b)
		if _, err = mc.FPutObject(ctx, bucket, "clip/"+r.Category+".jpg", path, minio.PutObjectOptions{ContentType: typ}); err != nil {
			log.Fatal(err)
		}
	}
	var added int
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(20261114)").Error; err != nil {
			return err
		}
		now := time.Now()
		for _, r := range rows {
			var count int64
			if err := tx.Model(&ds.CloudTariff{}).Where("tariff_name = ?", r.Name).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			} // Repeating the seed never overwrites edited demo rows.
			row := ds.CloudTariff{TariffName: r.Name, ShortDescription: r.Description, PricePerMonth: r.Price, RAMGB: r.RAM, TariffStatus: ds.StatusPublished, CreatorID: owner, ImageURL: "clip/" + r.Category + ".jpg", VideoURL: "", FormedAt: &now}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			added++
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Added %d tariffs. Test images: ./test-images. Repeated execution preserves existing tariffs.", added)
}
