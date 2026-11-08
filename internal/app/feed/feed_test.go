package feed

import (
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"sync"
	"testing"
	"time"
)

type source struct {
	mu          sync.Mutex
	generations int
	deleted     map[uint]bool
	lastViewer  uint
}

func (s *source) RandomPublished(_ context.Context, n int) ([]ds.CloudTariff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generations++
	rows := make([]ds.CloudTariff, n)
	for i := range rows {
		rows[i] = ds.CloudTariff{TariffID: uint(i + 1 + (s.generations-1)*100), TariffName: fmt.Sprint("tariff", i), TariffStatus: ds.StatusPublished}
	}
	return rows, nil
}
func (s *source) PublishedCard(_ context.Context, id, viewer uint) (*ds.CloudTariff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastViewer = viewer
	if s.deleted[id] {
		return nil, repository.ErrTariffNotFound
	}
	return &ds.CloudTariff{TariffID: id, TariffStatus: ds.StatusPublished, CreatorID: viewer, IsLiked: 1, LikeCount: 7}, nil
}
func fixture(t *testing.T) (*Service, *source, *miniredis.Miniredis) {
	t.Helper()
	m := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: m.Addr()})
	t.Cleanup(func() { c.Close() })
	src := &source{deleted: map[uint]bool{}}
	return &Service{Client: c, Source: src}, src, m
}
func TestHundredAndRollover(t *testing.T) {
	s, src, _ := fixture(t)
	ctx := context.Background()
	_, meta, err := s.Card(ctx, "browser", 0, Query{Init: true})
	if err != nil {
		t.Fatal(err)
	}
	values, err := s.Client.LRange(ctx, meta.RedisKey, 0, -1).Result()
	if err != nil || len(values) != 100 {
		t.Fatalf("%d %v", len(values), err)
	}
	seen := map[uint]bool{}
	for i, v := range values {
		var item Item
		if err = json.Unmarshal([]byte(v), &item); err != nil {
			t.Fatal(err)
		}
		if item.Position != i+1 || seen[item.Tariff.TariffID] {
			t.Fatal("bad positions/duplicates")
		}
		seen[item.Tariff.TariffID] = true
	}
	for i := 1; i <= 101; i++ {
		row, m, e := s.Card(ctx, "browser", 0, Query{Position: i})
		if e != nil || m.Position != i || row.TariffID != uint(i) {
			t.Fatalf("position=%d meta=%+v err=%v", i, m, e)
		}
	}
	if src.generations != 2 {
		t.Fatalf("generated %d times", src.generations)
	}
	row, second, e := s.Card(ctx, "browser", 0, Query{Position: 101})
	if e != nil || row.TariffID != 101 || second.BatchStart != 101 || second.BatchID == meta.BatchID || src.generations != 2 {
		t.Fatalf("rollover/retry failed: %+v %v", second, e)
	}
	if n := s.Client.LLen(ctx, meta.RedisKey).Val(); n != 100 {
		t.Fatalf("new batch len %d", n)
	}
}
func TestIsolationFreshnessAndExpiry(t *testing.T) {
	s, src, m := fixture(t)
	ctx := context.Background()
	_, guest, e := s.Card(ctx, "a", 0, Query{})
	if e != nil {
		t.Fatal(e)
	}
	row, user, e := s.Card(ctx, "a", 42, Query{})
	if e != nil || guest.RedisKey == user.RedisKey || src.lastViewer != 42 || row.IsLiked != 1 || row.LikeCount != 7 {
		t.Fatal("wrong viewer/isolation")
	}
	_, other, e := s.Card(ctx, "b", 0, Query{})
	if e != nil || other.RedisKey == guest.RedisKey {
		t.Fatal("browser sessions mixed")
	}
	src.deleted[1] = true
	row, skipped, e := s.Card(ctx, "a", 0, Query{Position: 1})
	if e != nil || skipped.Position != 2 || row.TariffID != 2 {
		t.Fatal("deleted card exposed")
	}
	m.FastForward(TTL + time.Second)
	_, fresh, e := s.Card(ctx, "a", 0, Query{Init: true})
	if e != nil || fresh.BatchID == guest.BatchID || fresh.BatchStart != 1 {
		t.Fatal("TTL did not recreate batch")
	}
}
func TestConcurrentInitialization(t *testing.T) {
	s, src, _ := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := s.Card(context.Background(), "same-browser", 0, Query{Init: true})
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if src.generations != 1 {
		t.Fatalf("concurrent initialization generated %d batches", src.generations)
	}
}

type emptySource struct{}

func (emptySource) RandomPublished(context.Context, int) ([]ds.CloudTariff, error) {
	return []ds.CloudTariff{}, nil
}
func (emptySource) PublishedCard(context.Context, uint, uint) (*ds.CloudTariff, error) {
	return nil, repository.ErrTariffNotFound
}
func TestEmptyAndUnavailable(t *testing.T) {
	s, _, m := fixture(t)
	s.Source = emptySource{}
	row, meta, e := s.Card(context.Background(), "empty", 0, Query{Init: true})
	if e != nil || row != nil || meta.BatchSize != 0 {
		t.Fatalf("empty feed %+v %v", meta, e)
	}
	if _, _, e = s.Card(context.Background(), "empty", 0, Query{ID: 7}); !errors.Is(e, repository.ErrTariffNotFound) {
		t.Fatalf("missing exact card: %v", e)
	}
	m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, e = s.Card(ctx, "empty", 0, Query{}); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("Redis failure not reported: %v", e)
	}
}
