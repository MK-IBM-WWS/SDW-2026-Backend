package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/feed"
	"context"
	"encoding/json"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"net/http/httptest"
	"testing"
)

type paginationRepo struct {
	Repository
	limit, offset int
	max           *int
}

func (r *paginationRepo) GetPublishedTariffs(_ context.Context, max *int, _ uint, limit, offset int) ([]ds.CloudTariff, int64, float64, error) {
	r.limit = limit
	r.offset = offset
	r.max = max
	return []ds.CloudTariff{}, 123, 1.2, nil
}
func (r *paginationRepo) PriceIndexEnabled(context.Context) (bool, error) { return true, nil }
func TestListBoundsAndMetadata(t *testing.T) {
	r := &paginationRepo{}
	h := NewHandler(r)
	for _, q := range []string{"?page=0", "?page=-1", "?limit=101", "?limit=0", "?page=abc", "?page=", "?page=1&page=2", "?priceLimit=", "?priceLimit=-1", "?unknown=1", "?page=2147483647&limit=100"} {
		w := call(t, "GET", "/api/tariffs/list"+q, "", h.ListAPI)
		if w.Code != 400 {
			t.Fatalf("%s returned %d", q, w.Code)
		}
	}
	w := call(t, "GET", "/api/tariffs/list?page=3&limit=10&priceLimit=1000", "", h.ListAPI)
	var data ListEnvelope
	json.Unmarshal(w.Body.Bytes(), &data)
	if w.Code != 200 || r.limit != 10 || r.offset != 20 || r.max == nil || *r.max != 1000 || data.Total != 123 || data.Page != 3 || data.QueryMS != 1.2 || !data.IndexEnabled {
		t.Fatalf("invalid pagination %s", w.Body.String())
	}
}

type feedRepo struct{ Repository }

func (r *feedRepo) RandomPublished(context.Context, int) ([]ds.CloudTariff, error) {
	return []ds.CloudTariff{{TariffID: 7, TariffName: "seven", TariffStatus: ds.StatusPublished}, {TariffID: 3, TariffName: "three", TariffStatus: ds.StatusPublished}}, nil
}
func (r *feedRepo) PublishedCard(_ context.Context, id, viewer uint) (*ds.CloudTariff, error) {
	return &ds.CloudTariff{TariffID: id, TariffName: "test", TariffStatus: ds.StatusPublished}, nil
}
func TestFeedCookieAndRedisOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer c.Close()
	repo := &feedRepo{}
	h := NewHandler(repo)
	h.Feed = &feed.Service{Client: c, Source: repo}
	router := gin.New()
	router.GET("/api/tariffs/feed", h.FeedAPI)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/tariffs/feed?init=true", nil))
	cookies := w.Result().Cookies()
	if w.Code != 200 || len(cookies) != 1 || !cookies[0].HttpOnly {
		t.Fatalf("session cookie missing %s", w.Body.String())
	}
	req := httptest.NewRequest("GET", "/api/tariffs/feed?id=7&next=true", nil)
	req.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var data FeedEnvelope
	json.Unmarshal(w.Body.Bytes(), &data)
	if w.Code != 200 || data.Data.TariffID != 3 || data.Feed.Position != 2 {
		t.Fatalf("Redis order not used %s", w.Body.String())
	}
	for _, q := range []string{"?next=true", "?id=0", "?id=abc", "?id=7&next=1", "?position=0", "?position=1&id=7", "?init=true&id=7", "?unknown=1", "?position=1&position=2"} {
		w = call(t, "GET", "/api/tariffs/feed"+q, "", h.FeedAPI)
		if w.Code != 400 {
			t.Fatalf("%s returned %d", q, w.Code)
		}
	}
}
