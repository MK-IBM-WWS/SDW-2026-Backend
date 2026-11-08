package repository

import (
	"cloud-tariffs-backend/internal/app/ds"
	"context"
	"testing"
)

func TestPageFilteringAndStableOrder(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		add(t, r, 1, ds.StatusPublished, 100)
	}
	add(t, r, 1, ds.StatusPublished, 2000)
	add(t, r, 2, ds.StatusDraft, 1)
	add(t, r, 2, ds.StatusDeleted, 1)
	max := 1000
	seen := map[uint]bool{}
	for page := 0; page < 3; page++ {
		rows, total, ms, e := r.GetPublishedTariffs(ctx, &max, 1, 10, page*10)
		want := 10
		if page == 2 {
			want = 5
		}
		if e != nil || total != 25 || len(rows) != want || ms < 0 {
			t.Fatalf("page %d: len=%d total=%d ms=%f %v", page, len(rows), total, ms, e)
		}
		for _, row := range rows {
			if seen[row.TariffID] || row.PricePerMonth > max || row.TariffStatus != ds.StatusPublished {
				t.Fatal("duplicate/unfiltered card")
			}
			seen[row.TariffID] = true
		}
	}
	rows, total, _, e := r.GetPublishedTariffs(ctx, &max, 0, 10, 100)
	if e != nil || total != 25 || len(rows) != 0 {
		t.Fatal("page beyond end")
	}
	if _, _, _, e = r.GetPublishedTariffs(ctx, nil, 0, 101, 0); e == nil {
		t.Fatal("unbounded limit accepted")
	}
}
