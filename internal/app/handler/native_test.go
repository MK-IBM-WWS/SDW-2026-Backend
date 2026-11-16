package handler

import (
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/repository"
	"context"
	"io"
	"strings"
	"testing"
)

type nativeFake struct {
	Repository
	row  ds.CloudTariff
	q    string
	demo bool
}

func (r *nativeFake) NativeCatalog(_ context.Context, q string, _ *int, demo bool, _, _ int) ([]ds.CloudTariff, int64, error) {
	r.q = q
	r.demo = demo
	return []ds.CloudTariff{r.row}, 1, nil
}
func (r *nativeFake) OpenNativeImage(context.Context, string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("image")), "image/jpeg", nil
}
func (r *nativeFake) GetPublishedTariff(id, _ uint) (*ds.CloudTariff, error) {
	if id != r.row.TariffID {
		return nil, repository.ErrTariffNotFound
	}
	return &r.row, nil
}
func TestNativeGuestValidationAndDetails(t *testing.T) {
	repo := &nativeFake{row: ds.CloudTariff{TariffID: 7, TariffName: "CLIP-cat-01", ImageURL: "clip/cat.jpg"}}
	h := NewHandler(repo)
	for _, q := range []string{"?page=0", "?limit=101", "?q=x&q=y", "?demo=maybe", "?priceLimit=-1", "?priceLimit=1.5", "?unknown=x"} {
		w := call(t, "GET", "/api/tariffs/7"+q, "", h.NativeListAPI)
		if w.Code != 400 {
			t.Fatalf("%s: %d", q, w.Code)
		}
	}
	w := call(t, "GET", "/api/tariffs/7?q=cat&demo=true", "", h.NativeListAPI)
	if w.Code != 200 || repo.q != "cat" || !repo.demo || !strings.Contains(w.Body.String(), "/api/native/tariffs/7/image") {
		t.Fatal(w.Body.String())
	}
	if w = call(t, "GET", "/api/tariffs/7", "", h.NativeDetailAPI); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = call(t, "GET", "/api/tariffs/8", "", h.NativeDetailAPI); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w = call(t, "GET", "/api/tariffs/7", "", h.NativeImageAPI); w.Code != 200 || w.Body.String() != "image" {
		t.Fatal(w.Code, w.Body.String())
	}
}
