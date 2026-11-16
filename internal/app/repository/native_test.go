package repository

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
	"testing"
)

func TestNativeCatalogAndFreshDatabaseUpdates(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	rows := []ds.CloudTariff{
		{TariffName: "CLIP-cat-01", ShortDescription: "A cat image", TariffStatus: ds.StatusPublished, PricePerMonth: 500, CreatorID: 1},
		{TariffName: "CLIP-dog-01", ShortDescription: "A dog image", TariffStatus: ds.StatusPublished, PricePerMonth: 2000, CreatorID: 1},
		{TariffName: "CLIP-cat-draft", TariffStatus: ds.StatusDraft, CreatorID: 2},
		{TariffName: "CLIP-cat-deleted", TariffStatus: ds.StatusDeleted, CreatorID: 1},
		{TariffName: "Old unrelated tariff", TariffStatus: ds.StatusPublished, CreatorID: 1},
	}
	for i := range rows {
		if err := repo.db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	max := 1000
	found, total, err := repo.NativeCatalog(ctx, "CAT", &max, true, 100, 0)
	if err != nil || total != 1 || len(found) != 1 || found[0].TariffName != "CLIP-cat-01" {
		t.Fatalf("filter: %v %d %v", found, total, err)
	}
	// Literal user wildcards and SQL injection attempts do not widen the result.
	for _, q := range []string{"%", "_", "' OR 1=1 --"} {
		_, total, err = repo.NativeCatalog(ctx, q, nil, false, 100, 0)
		if err != nil || total != 0 {
			t.Fatalf("unsafe search %q: %d %v", q, total, err)
		}
	}
	if err := repo.db.Model(&ds.CloudTariff{}).Where("tariff_id = ?", rows[0].TariffID).Updates(map[string]any{"price_per_month": 999, "short_description": "Changed database description"}).Error; err != nil {
		t.Fatal(err)
	}
	found, _, err = repo.NativeCatalog(ctx, "changed", nil, true, 100, 0)
	if err != nil || len(found) != 1 || found[0].PricePerMonth != 999 {
		t.Fatal("DB edits must be immediately visible", found, err)
	}
	_, total, err = repo.NativeCatalog(ctx, "", nil, true, 1, 1)
	if err != nil || total != 2 {
		t.Fatal("pagination metadata", total, err)
	}
}

func TestNativeSearchCatalogUsesAllEligibleDescriptionsAndRealLikes(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	english := "A photo of a red car parked on a city street, for cloud image recognition."
	records := []ds.CloudTariff{
		{TariffName: "Original tariff without CLIP prefix", ShortDescription: english, TariffStatus: ds.StatusPublished, CreatorID: 1},
		{TariffName: "CLIP-valid", ShortDescription: english, TariffStatus: ds.StatusPublished, CreatorID: 1},
		{TariffName: "Old Russian tariff", ShortDescription: "Облачный сервер для хранения данных и распознавания изображений автомобилей.", TariffStatus: ds.StatusPublished, CreatorID: 1},
		{TariffName: "Short", ShortDescription: "A car", TariffStatus: ds.StatusPublished, CreatorID: 1},
		{TariffName: "Deleted", ShortDescription: english, TariffStatus: ds.StatusDeleted, CreatorID: 1},
	}
	for i := range records {
		if err := repo.db.Create(&records[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.db.Exec("INSERT INTO user_tariff_likes(user_id,tariff_id) VALUES(?,?)", 2, records[0].TariffID).Error; err != nil {
		t.Fatal(err)
	}
	rows, total, err := repo.NativeSearchCatalog(ctx, "", 100, 0)
	if err != nil || total != 2 || len(rows) != 2 || rows[0].LikeCount != 1 {
		t.Fatalf("corpus=%v total=%d err=%v", rows, total, err)
	}
	rows, total, err = repo.NativeSearchCatalog(ctx, "original", 100, 0)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatal(rows, total, err)
	}
}
