package repository

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cloud-tariffs-backend/internal/app/ds"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

func TestPublishMediaPreservesIDAndCompensates(t *testing.T) {
	for _, mode := range []string{"both", "image", "video", "unchanged", "upload_failure", "database_failure", "foreign", "published", "shared_old"} {
		t.Run(mode, func(t *testing.T) {
			r := testRepo(t)
			tariff := add(t, r, 1, ds.StatusDraft, 100)
			if err := r.db.Model(&tariff).Updates(map[string]any{"image_url": "old.jpg", "video_url": "old.mp4"}).Error; err != nil {
				t.Fatal(err)
			}
			if mode == "published" {
				r.db.Model(&tariff).Update("tariff_status", ds.StatusPublished)
			}
			if mode == "shared_old" {
				other := add(t, r, 2, ds.StatusPublished, 100)
				r.db.Model(&other).Update("image_url", "old.jpg")
			}
			var mu sync.Mutex
			objects := map[string]bool{"/bucket/old.jpg": true, "/bucket/old.mp4": true}
			puts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch req.Method {
				case "PUT":
					io.Copy(io.Discard, req.Body)
					puts++
					if mode == "upload_failure" && strings.Contains(req.URL.Path, "video_") {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(403)
						io.WriteString(w, `<Error><Code>AccessDenied</Code></Error>`)
						return
					}
					objects[req.URL.Path] = true
					w.Header().Set("ETag", `"test"`)
				case "DELETE":
					delete(objects, req.URL.Path)
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected S3 call %s", req.Method)
					w.WriteHeader(500)
				}
			}))
			defer srv.Close()
			client, err := minio.New(strings.TrimPrefix(srv.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test", "testpassword", ""), Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			r.minio = client
			r.bucket = "bucket"
			var data bytes.Buffer
			writer := multipart.NewWriter(&data)
			part, _ := writer.CreateFormFile("image", "new.jpg")
			part.Write([]byte("test content"))
			writer.Close()
			req := httptest.NewRequest("POST", "/", &data)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			req.ParseMultipartForm(1024)
			defer req.MultipartForm.RemoveAll()
			img := &Upload{Header: req.MultipartForm.File["image"][0], Type: "image/jpeg", Extension: ".jpg"}
			vid := &Upload{Header: img.Header, Type: "video/mp4", Extension: ".mp4"}
			in := PublishTariffInput{TariffID: tariff.TariffID, CreatorID: 1, TariffName: "Updated", ShortDescription: "Updated description", PricePerMonth: 750, RAMGB: 8, Context: context.Background(), Image: img, Video: vid}
			if mode == "image" {
				in.Video = nil
			}
			if mode == "video" {
				in.Image = nil
			}
			if mode == "unchanged" {
				in.Image = nil
				in.Video = nil
			}
			if mode == "foreign" {
				in.CreatorID = 2
			}
			if mode == "database_failure" {
				r.db.Callback().Update().Before("gorm:update").Register("fail_publication", func(tx *gorm.DB) { tx.AddError(errors.New("DB failed")) })
			}
			err = r.PublishDraftTariff(in)
			failure := mode == "upload_failure" || mode == "database_failure" || mode == "foreign" || mode == "published"
			if (err != nil) != failure {
				t.Fatalf("unexpected result: %v", err)
			}
			var got ds.CloudTariff
			r.db.First(&got, tariff.TariffID)
			var count int64
			r.db.Model(&ds.CloudTariff{}).Where("creator_id = ?", 1).Count(&count)
			if count != 1 || got.TariffID != tariff.TariffID || got.CreatorID != 1 {
				t.Fatal("tariff ID/owner changed or extra row inserted")
			}
			mu.Lock()
			defer mu.Unlock()
			if failure {
				if got.ImageURL != "old.jpg" || got.VideoURL != "old.mp4" || len(objects) != 2 {
					t.Fatalf("failed publication changed files: %+v %v", got, objects)
				}
				if mode != "published" && got.TariffStatus != ds.StatusDraft {
					t.Fatal("failed request changed status")
				}
				if (mode == "foreign" || mode == "published") && puts != 0 {
					t.Fatal("uploaded before ownership/state check")
				}
			} else {
				if got.TariffStatus != ds.StatusPublished || got.PricePerMonth != 750 {
					t.Fatal("not published")
				}
				if (in.Image == nil) != (got.ImageURL == "old.jpg") || (in.Video == nil) != (got.VideoURL == "old.mp4") {
					t.Fatal("wrong files replaced")
				}
				if !objects["/bucket/"+got.ImageURL] || !objects["/bucket/"+got.VideoURL] {
					t.Fatal("persisted filename missing in S3")
				}
				expected := 2
				if mode == "shared_old" {
					expected = 3
				}
				if len(objects) != expected {
					t.Fatalf("incorrect cleanup: %v", objects)
				}
			}
		})
	}
}
