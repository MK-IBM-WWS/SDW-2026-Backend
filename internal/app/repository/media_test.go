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

func TestUploadsCompensatedOnFailure(t *testing.T) {
	for _, failure := range []string{"second_upload", "database"} {
		t.Run(failure, func(t *testing.T) {
			r := testRepo(t)
			var mu sync.Mutex
			objects := map[string]bool{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch req.Method {
				case "PUT":
					io.Copy(io.Discard, req.Body)
					if failure == "second_upload" && strings.Contains(req.URL.Path, "video_") {
						w.Header().Set("Content-Type", "application/xml")
						w.WriteHeader(403)
						io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>test</Message></Error>`)
						return
					}
					objects[req.URL.Path] = true
					w.Header().Set("ETag", `"d41d8cd98f00b204e9800998ecf8427e"`)
					w.WriteHeader(200)
				case "DELETE":
					delete(objects, req.URL.Path)
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected S3 request %s %s", req.Method, req.URL)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("test", "testpassword", ""), Region: "us-east-1"})
			if err != nil {
				t.Fatal(err)
			}
			r.minio = client
			r.bucket = "test-bucket"
			if failure == "database" {
				r.db.Callback().Create().Before("gorm:create").Register("test_fail", func(tx *gorm.DB) { tx.AddError(errors.New("DB unavailable")) })
			}
			var data bytes.Buffer
			writer := multipart.NewWriter(&data)
			part, _ := writer.CreateFormFile("image", "image.jpg")
			part.Write([]byte("test upload payload"))
			writer.Close()
			req := httptest.NewRequest("POST", "/", &data)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err = req.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			defer req.MultipartForm.RemoveAll()
			upload := &Upload{Header: req.MultipartForm.File["image"][0], Type: "image/jpeg", Extension: ".jpg"}
			tariff := ds.CloudTariff{TariffName: "Test", CreatorID: 1, TariffStatus: ds.StatusDraft}
			if err = r.CreateWithFiles(context.Background(), &tariff, upload, upload); err == nil {
				t.Fatal("failure was ignored")
			}
			mu.Lock()
			n := len(objects)
			mu.Unlock()
			if n != 0 {
				t.Fatalf("orphan files: %d", n)
			}
			var count int64
			r.db.Model(&ds.CloudTariff{}).Count(&count)
			if count != 0 {
				t.Fatal("partial tariff persisted")
			}
		})
	}
}
