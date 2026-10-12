package api

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloud-tariffs-backend/internal/app/auth"
	"cloud-tariffs-backend/internal/app/ds"
	"cloud-tariffs-backend/internal/app/handler"
	"cloud-tariffs-backend/internal/app/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type usersRepo struct {
	handler.Repository
	users map[string]ds.User
	draft *ds.CloudTariff
}

func (r *usersRepo) Register(u *ds.User) error {
	if _, ok := r.users[u.Login]; ok {
		return repository.ErrLoginExists
	}
	u.UserID = uint(len(r.users) + 1)
	r.users[u.Login] = *u
	return nil
}
func (r *usersRepo) GetUserByLogin(login string) (ds.User, error) {
	if u, ok := r.users[login]; ok {
		return u, nil
	}
	return ds.User{}, gorm.ErrRecordNotFound
}
func (r *usersRepo) GetUser(id uint) (ds.User, error) {
	for _, u := range r.users {
		if u.UserID == id {
			return u, nil
		}
	}
	return ds.User{}, gorm.ErrRecordNotFound
}
func (r *usersRepo) GetPublishedTariffs(_ *int, viewer uint) ([]ds.CloudTariff, error) {
	return []ds.CloudTariff{{TariffID: 3, CreatorID: 2, Creator: r.users["second"]}}, nil
}
func (r *usersRepo) CreateWithFiles(_ context.Context, t *ds.CloudTariff, _, _ *repository.Upload) error {
	t.TariffID = 5
	v := *t
	r.draft = &v
	return nil
}
func (r *usersRepo) GetDraftTariff(user uint) (*ds.CloudTariff, error) {
	if r.draft == nil || r.draft.CreatorID != user {
		return nil, nil
	}
	return r.draft, nil
}
func httpCall(r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestRegisteredUserJWTAndRouteAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer client.Close()
	a, err := auth.New("0123456789abcdef0123456789abcdef", time.Hour, auth.RedisBlacklist{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	repo := &usersRepo{users: map[string]ds.User{}}
	h := handler.NewHandler(repo)
	h.Auth = a
	r := gin.New()
	RegisterAPI(r, h, a)
	expected := map[string]bool{
		"GET /api/tariffs": true, "GET /api/tariffs/feed": true,
		"GET /api/tariffs/draft": true, "POST /api/tariffs": true,
		"PUT /api/tariffs/:id/publication": true, "DELETE /api/tariffs/:id": true,
		"POST /api/tariffs/:id/likes": true, "POST /api/users": true,
		"POST /api/users/login": true, "POST /api/users/logout": true,
	}
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if !expected[key] {
			t.Fatalf("extra route: %s", key)
		}
		delete(expected, key)
	}
	if len(expected) != 0 {
		t.Fatalf("missing routes: %v", expected)
	}
	for _, path := range []string{"/api/tariffs/3", "/api/users/1/tariffs", "/api/users/me", "/api/tariffs/3/image", "/api/tariffs/3/video"} {
		if w := httpCall(r, "GET", path, "", ""); w.Code != 404 {
			t.Fatalf("removed route still available: %s", path)
		}
	}

	for _, user := range []string{"first", "second"} {
		w := httpCall(r, "POST", "/api/users", `{"login":"`+user+`","password":"password123"}`, "")
		if w.Code != 201 || strings.Contains(w.Body.String(), "password123") {
			t.Fatalf("register: %d %s", w.Code, w.Body.String())
		}
		if repo.users[user].PasswordHash == "password123" || repo.users[user].IsModerator {
			t.Fatal("unsafe registration")
		}
	}
	if w := httpCall(r, "POST", "/api/users", `{"login":"first","password":"password123","is_moderator":true}`, ""); w.Code != 400 {
		t.Fatal("role escalation accepted")
	}
	if w := httpCall(r, "POST", "/api/users", `{"login":"first","password":"password123"}`, ""); w.Code != 409 {
		t.Fatal("duplicate login accepted")
	}
	for _, body := range []string{`{"login":"first","password":"wrong123"}`, `{"login":"unknown","password":"password123"}`} {
		if w := httpCall(r, "POST", "/api/users/login", body, ""); w.Code != 401 {
			t.Fatal("invalid credentials accepted")
		}
	}
	w := httpCall(r, "POST", "/api/users/login", `{"login":" SECOND ","password":"password123"}`, "")
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatal("login must not set a cookie")
	}
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var login struct {
		Data handler.TokenResponse `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &login)
	token := login.Data.AccessToken
	claims, err := a.Validate(context.Background(), token)
	if err != nil || claims.UserID != 2 || login.Data.ExpiresIn != 3600 || login.Data.Role != auth.Creator {
		t.Fatal("wrong JWT identity")
	}
	for _, tc := range []struct{ method, path string }{{"POST", "/api/tariffs"}, {"PUT", "/api/tariffs/3/publication"}, {"DELETE", "/api/tariffs/3"}, {"POST", "/api/tariffs/3/likes"}, {"GET", "/api/tariffs/draft"}, {"POST", "/api/users/logout"}} {
		if w := httpCall(r, tc.method, tc.path, "", ""); w.Code != 401 {
			t.Fatalf("unprotected %s %s: %d", tc.method, tc.path, w.Code)
		}
	}
	for _, tc := range []struct {
		token string
		owner float64
	}{{"", 0}, {token, 1}, {"invalid", 0}} {
		w := httpCall(r, "GET", "/api/tariffs", "", tc.token)
		var response struct {
			Data []map[string]any `json:"data"`
		}
		json.Unmarshal(w.Body.Bytes(), &response)
		if w.Code != 200 || len(response.Data) != 1 || response.Data[0]["is_owner"] != tc.owner {
			t.Fatalf("owner response %s", w.Body.String())
		}
		if _, exists := response.Data[0]["creator"].(map[string]any)["is_moderator"]; exists {
			t.Fatal("creator DTO changed")
		}
	}
	// A real HTTP multipart create must use the authenticated user, even when it is not ID 1.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("tariff_name", "My draft")
	writer.Close()
	req := httptest.NewRequest("POST", "/api/tariffs", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 201 || repo.draft == nil || repo.draft.CreatorID != 2 {
		t.Fatalf("automatic owner: %d %s", w.Code, w.Body.String())
	}
	w = httpCall(r, "GET", "/api/tariffs/draft", "", token)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"creator_id":2`) {
		t.Fatal("own draft unavailable")
	}

	publication := `{"tariff_name":"Published","short_description":"Updated","price_per_month":900,"ram_gb":8}`
	if w = httpCall(r, "PUT", "/api/tariffs/5/publication", publication, ""); w.Code != 401 {
		t.Fatal("guest publication accepted")
	}
	if w = httpCall(r, "PUT", "/api/tariffs/5/publication", publication, token); w.Code != 200 || repo.draft.PricePerMonth != 900 || repo.draft.RAMGB != 8 || repo.draft.TariffStatus != ds.StatusPublished {
		t.Fatalf("publication failed: %d %s", w.Code, w.Body.String())
	}
	w = httpCall(r, "POST", "/api/users/logout", "", token)
	if w.Code != 200 {
		t.Fatal("logout failed")
	}
	if w = httpCall(r, "GET", "/api/tariffs/draft", "", token); w.Code != 401 {
		t.Fatal("logged out token still works")
	}
	if w = httpCall(r, "GET", "/api/tariffs", "", ""); w.Code != 200 {
		t.Fatal("public read unavailable after logout")
	}
	if w = httpCall(r, "GET", "/api/tariffs", "", token); w.Code != 200 {
		t.Fatal("public reads must work with revoked JWT")
	}
	full := NewRouter(h, a)
	for _, path := range []string{"/tariffs", "/tariffs/feed", "/tariffs/draft", "/auth", "/static/styles/style.css", "/static/scripts/auth.js"} {
		if w = httpCall(full, "GET", path, "", ""); w.Code != 404 {
			t.Fatalf("frontend route %s remains: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/swagger/index.html", "/swagger/doc.json"} {
		if w = httpCall(full, "GET", path, "", ""); w.Code != 200 {
			t.Fatalf("Swagger %s: %d", path, w.Code)
		}
	}
	w = httpCall(full, "GET", "/swagger/doc.json", "", "")
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	apiRoutes := map[string]bool{}
	for _, route := range r.Routes() {
		apiRoutes[strings.ToLower(route.Method)+" "+strings.ReplaceAll(strings.TrimPrefix(route.Path, "/api"), ":id", "{id}")] = true
	}
	for path, methods := range spec.Paths {
		for method := range methods {
			key := method + " " + path
			if !apiRoutes[key] {
				t.Fatalf("extra Swagger method: %s", key)
			}
			delete(apiRoutes, key)
		}
	}
	if len(apiRoutes) != 0 {
		t.Fatalf("undocumented API methods: %v", apiRoutes)
	}
	if w = httpCall(full, "GET", "/", "", ""); w.Code != 302 || w.Header().Get("Location") != "/swagger/index.html" {
		t.Fatal("root must open Swagger")
	}

}

func (r *usersRepo) PublishDraftTariff(in repository.PublishTariffInput) error {
	if r.draft == nil || r.draft.TariffID != in.TariffID {
		return repository.ErrTariffNotFound
	}
	if r.draft.CreatorID != in.CreatorID {
		return repository.ErrForbidden
	}
	r.draft.TariffName = in.TariffName
	r.draft.ShortDescription = in.ShortDescription
	r.draft.PricePerMonth = in.PricePerMonth
	r.draft.RAMGB = in.RAMGB
	r.draft.TariffStatus = ds.StatusPublished
	return nil
}
func (r *usersRepo) GetPublishedTariff(id, viewer uint) (*ds.CloudTariff, error) {
	if r.draft == nil || r.draft.TariffID != id || r.draft.TariffStatus != ds.StatusPublished {
		return nil, repository.ErrTariffNotFound
	}
	return r.draft, nil
}
