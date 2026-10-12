package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cloud-tariffs-backend/internal/app/ds"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testRepo(t *testing.T) *Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&ds.User{}, &ds.CloudTariff{}, &ds.UserTariffLike{}); err != nil {
		t.Fatal(err)
	}
	if err = db.Exec("CREATE UNIQUE INDEX idx_cloud_tariffs_one_draft_per_creator ON cloud_tariffs (creator_id) WHERE tariff_status = 'черновик'").Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range []ds.User{{UserID: 1, Login: "student", PasswordHash: "hash"}, {UserID: 2, Login: "other", PasswordHash: "hash"}} {
		if err = db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	return &Repository{db: db}
}
func add(t *testing.T, r *Repository, owner uint, status ds.TariffStatus, price int) ds.CloudTariff {
	t.Helper()
	v := ds.CloudTariff{TariffName: "Test", CreatorID: owner, TariffStatus: status, PricePerMonth: price, RAMGB: 4}
	if err := r.db.Create(&v).Error; err != nil {
		t.Fatal(err)
	}
	return v
}
func TestLifecycleAndVisibility(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	own := add(t, r, 1, ds.StatusDraft, 100)
	other := add(t, r, 2, ds.StatusPublished, 200)
	deleted := add(t, r, 1, ds.StatusDeleted, 1)
	if _, err := r.GetPublishedTariff(own.TariffID, 1); !errors.Is(err, ErrTariffNotFound) {
		t.Fatalf("draft exposed: %v", err)
	}
	if _, err := r.GetPublishedTariff(deleted.TariffID, 1); !errors.Is(err, ErrTariffNotFound) {
		t.Fatal("deleted exposed")
	}
	if err := r.DeleteTariff(ctx, other.TariffID, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign deletion: %v", err)
	}
	in := PublishTariffInput{TariffID: own.TariffID, CreatorID: 1, TariffName: "Published", ShortDescription: "desc", PricePerMonth: 100, RAMGB: 4}
	if err := r.PublishDraftTariff(in); err != nil {
		t.Fatal(err)
	}
	if err := r.PublishDraftTariff(in); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("republish: %v", err)
	}
	pub, err := r.GetPublishedTariff(own.TariffID, 1)
	if err != nil || pub.FormedAt == nil || pub.Creator.Login != "student" {
		t.Fatalf("publish/creator failed: %+v %v", pub, err)
	}
	cap := 150
	list, err := r.GetPublishedTariffs(&cap, 1)
	if err != nil || len(list) != 1 || list[0].IsOwner != 1 {
		t.Fatalf("filter: %+v %v", list, err)
	}
	for _, v := range []int{1, 1} {
		n, err := r.SetLike(ctx, own.TariffID, 1, v)
		if err != nil || n != 1 {
			t.Fatalf("duplicate like: %d %v", n, err)
		}
	}
	for _, v := range []int{0, 0} {
		n, err := r.SetLike(ctx, own.TariffID, 1, v)
		if err != nil || n != 0 {
			t.Fatalf("unlike: %d %v", n, err)
		}
	}
	if err := r.DeleteTariff(ctx, own.TariffID, 1); err != nil {
		t.Fatal(err)
	}
	var persisted ds.CloudTariff
	if err := r.db.First(&persisted, own.TariffID).Error; err != nil || persisted.TariffStatus != ds.StatusDeleted {
		t.Fatal("not a soft delete")
	}
	if err := r.PublishDraftTariff(in); !errors.Is(err, ErrTariffNotFound) {
		t.Fatal("deleted republished")
	}
	if _, err := r.SetLike(ctx, own.TariffID, 1, 1); !errors.Is(err, ErrTariffNotFound) {
		t.Fatal("deleted liked")
	}
	if _, err := r.GetPublishedTariff(own.TariffID, 1); !errors.Is(err, ErrTariffNotFound) {
		t.Fatal("deleted tariff visible")
	}
}
func TestDraftIsolationAndUniqueIndex(t *testing.T) {
	r := testRepo(t)
	a := add(t, r, 1, ds.StatusDraft, 0)
	b := add(t, r, 2, ds.StatusDraft, 0)
	draft, err := r.GetDraftTariff(1)
	if err != nil || draft.TariffID != a.TariffID {
		t.Fatal("wrong draft")
	}
	if err := r.CreateDraftTariff(&ds.CloudTariff{TariffName: "second", CreatorID: 1, TariffStatus: ds.StatusDraft}); err == nil {
		t.Fatal("second draft allowed")
	}
	if _, err := r.GetPublishedTariff(b.TariffID, 1); !errors.Is(err, ErrTariffNotFound) {
		t.Fatal("foreign draft visible")
	}
	if err := r.PublishDraftTariff(PublishTariffInput{TariffID: b.TariffID, CreatorID: 1}); !errors.Is(err, ErrForbidden) {
		t.Fatal("foreign draft published")
	}
	if err := r.DeleteTariff(context.Background(), a.TariffID, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateDraftTariff(&ds.CloudTariff{TariffName: "new", CreatorID: 1, TariffStatus: ds.StatusDraft}); err != nil {
		t.Fatal(err)
	}
}
func TestFeedWrap(t *testing.T) {
	r := testRepo(t)
	a := add(t, r, 1, ds.StatusPublished, 100)
	b := add(t, r, 2, ds.StatusPublished, 200)
	add(t, r, 1, ds.StatusDeleted, 1)
	next, err := r.GetNextPublishedTariff(b.TariffID, 1)
	if err != nil || next.TariffID != a.TariffID {
		t.Fatal("feed wrap failed")
	}
}

func TestTwoUsersHaveSeparateLikesAndOwnership(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	item := add(t, r, 2, ds.StatusPublished, 100)
	if _, err := r.SetLike(ctx, item.TariffID, 1, 1); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user         uint
		liked, owner int
	}{{0, 0, 0}, {1, 1, 0}, {2, 0, 1}} {
		list, err := r.GetPublishedTariffs(nil, tc.user)
		if err != nil || len(list) != 1 || list[0].IsLiked != tc.liked || list[0].IsOwner != tc.owner || list[0].LikeCount != 1 {
			t.Fatalf("user %d: %+v %v", tc.user, list, err)
		}
	}
	if _, err := r.SetLike(ctx, item.TariffID, 2, 1); err != nil {
		t.Fatal(err)
	}
	if n, err := r.SetLike(ctx, item.TariffID, 1, 0); err != nil || n != 1 {
		t.Fatal("unlike affected another user")
	}
	if err := r.DeleteTariff(ctx, item.TariffID, 1); !errors.Is(err, ErrForbidden) {
		t.Fatal("foreign deletion allowed")
	}
	if err := r.DeleteTariff(ctx, item.TariffID, 2); err != nil {
		t.Fatal("real owner cannot delete")
	}
}
