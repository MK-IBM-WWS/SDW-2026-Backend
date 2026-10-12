package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
	"errors"
	"testing"
)

type mediaRepository struct {
	Repository
	missing bool
	calls   int
}

func (r *mediaRepository) MediaURL(_ context.Context, name string) (string, error) {
	r.calls++
	if r.missing {
		return "", errors.New("missing object")
	}
	return "http://minio.example/test-bucket/" + name + "?X-Amz-Signature=test", nil
}
func TestMediaLinksAndFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name         string
		missing      bool
		calls        int
		image, video string
	}{
		{"", false, 0, fallbackImageURL, fallbackVideoURL},
		{"missing", true, 2, fallbackImageURL, fallbackVideoURL},
		{"existing", false, 2, "http://minio.example/test-bucket/existing.jpg?X-Amz-Signature=test", "http://minio.example/test-bucket/existing.mp4?X-Amz-Signature=test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mediaRepository{missing: tc.missing}
			tariff := ds.CloudTariff{TariffID: 7}
			if tc.name != "" {
				tariff.ImageURL = tc.name + ".jpg"
				tariff.VideoURL = tc.name + ".mp4"
			}
			dto := NewHandler(repo).serialize(context.Background(), tariff, 0)
			if dto.ImageURL != tc.image || dto.VideoURL != tc.video || repo.calls != tc.calls {
				t.Fatalf("unexpected media: %+v, calls=%d", dto, repo.calls)
			}
		})
	}
}
