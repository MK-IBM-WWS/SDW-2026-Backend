package repository

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

type TariffStatus string

const (
	StatusDraft     TariffStatus = "черновик"
	StatusPublished TariffStatus = "опубликован"
	StatusDeleted   TariffStatus = "удалён"
)

type CloudTariff struct {
	TariffID         int
	TariffName       string
	ShortDescription string
	PricePerMonth    int
	RAMGB            int
	ImageKey         string
	VideoKey         string
	TariffStatus     TariffStatus
	LikedByUserIDs   []int
}

func (t CloudTariff) LikeCount() int {
	return len(t.LikedByUserIDs)
}

func (t CloudTariff) DescriptionPreview() string {
	words := strings.Fields(t.ShortDescription)
	preview := ""

	for _, word := range words {
		candidate := word
		if preview != "" {
			candidate = preview + " " + word
		}

		if len([]rune(candidate)) > 20 {
			break
		}

		preview = candidate
	}

	if preview == "" && len(words) > 0 {
		word := []rune(words[0])
		if len(word) > 20 {
			word = word[:20]
		}
		preview = string(word)
	}

	return preview
}

func (t CloudTariff) ImageURL() string {
	return minioObjectURL(t.ImageKey)
}

func (t CloudTariff) VideoURL() string {
	return minioObjectURL(t.VideoKey)
}

func minioObjectURL(objectKey string) string {
	baseURL := strings.TrimRight(environmentOrDefault("MINIO_PUBLIC_URL", "http://localhost:9000"), "/")
	bucket := strings.Trim(environmentOrDefault("MINIO_BUCKET", "infrastructure"), "/")

	parts := strings.Split(strings.TrimLeft(objectKey, "/"), "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}

	return fmt.Sprintf("%s/%s/%s", baseURL, url.PathEscape(bucket), strings.Join(parts, "/"))
}

func environmentOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

type Repository struct {
	tariffs []CloudTariff
}

func NewRepository() (*Repository, error) {
	tariffs := seedTariffs()
	if len(tariffs) == 0 {
		return nil, fmt.Errorf("коллекция тарифов пуста")
	}

	return &Repository{tariffs: tariffs}, nil
}

func (r *Repository) GetPublishedTariffs() ([]CloudTariff, error) {
	result := make([]CloudTariff, 0)
	for _, tariff := range r.tariffs {
		if tariff.TariffStatus == StatusPublished {
			result = append(result, tariff)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TariffID < result[j].TariffID
	})

	return result, nil
}

func (r *Repository) GetTariffsByMaxPrice(maxPrice int) ([]CloudTariff, error) {
	tariffs, err := r.GetPublishedTariffs()
	if err != nil {
		return nil, err
	}

	result := make([]CloudTariff, 0)
	for _, tariff := range tariffs {
		if tariff.PricePerMonth <= maxPrice {
			result = append(result, tariff)
		}
	}

	return result, nil
}

func (r *Repository) GetPublishedTariff(tariffID int) (CloudTariff, error) {
	for _, tariff := range r.tariffs {
		if tariff.TariffID == tariffID && tariff.TariffStatus == StatusPublished {
			return tariff, nil
		}
	}

	return CloudTariff{}, fmt.Errorf("опубликованный тариф %d не найден", tariffID)
}

func (r *Repository) GetFirstTariff() (CloudTariff, error) {
	tariffs, err := r.GetPublishedTariffs()
	if err != nil {
		return CloudTariff{}, err
	}
	if len(tariffs) == 0 {
		return CloudTariff{}, fmt.Errorf("опубликованные тарифы не найдены")
	}

	return tariffs[0], nil
}

func (r *Repository) GetNextTariff(afterTariffID int) (CloudTariff, error) {
	tariffs, err := r.GetPublishedTariffs()
	if err != nil {
		return CloudTariff{}, err
	}
	if len(tariffs) == 0 {
		return CloudTariff{}, fmt.Errorf("опубликованные тарифы не найдены")
	}

	for _, tariff := range tariffs {
		if tariff.TariffID > afterTariffID {
			return tariff, nil
		}
	}

	return tariffs[0], nil
}

func (r *Repository) GetDraftTariff() (CloudTariff, error) {
	for _, tariff := range r.tariffs {
		if tariff.TariffStatus == StatusDraft {
			return tariff, nil
		}
	}

	return CloudTariff{}, fmt.Errorf("черновик тарифа не найден")
}
