package repository

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Repository struct {
	db     *gorm.DB
	minio  *minio.Client
	bucket string
}

func New(dsn string) (*Repository, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)
	client, err := minio.New(os.Getenv("MINIO_ENDPOINT"), &minio.Options{
		Creds:  credentials.NewStaticV4(os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"), ""),
		Secure: os.Getenv("MINIO_USE_SSL") == "true",
	})
	if err != nil {
		sqlDB.Close()
		return nil, err
	}
	r := &Repository{db: db, minio: client, bucket: os.Getenv("MINIO_BUCKET_NAME")}
	if r.bucket == "" {
		sqlDB.Close()
		return nil, fmt.Errorf("MINIO_BUCKET_NAME is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	exists, err := client.BucketExists(ctx, r.bucket)
	if err == nil && !exists {
		err = client.MakeBucket(ctx, r.bucket, minio.MakeBucketOptions{})
	}
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("MinIO bucket: %w", err)
	}
	return r, nil
}
func (r *Repository) Close() error {
	db, err := r.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
