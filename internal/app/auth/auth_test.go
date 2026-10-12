package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloud-tariffs-backend/internal/app/ds"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const secret = "0123456789abcdef0123456789abcdef0123456789abcdef"

func testService(t *testing.T) (*Service, *miniredis.Miniredis) {
	t.Helper()
	m := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: m.Addr(), MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { client.Close() })
	s, err := New(secret, time.Hour, RedisBlacklist{Client: client})
	if err != nil {
		t.Fatal(err)
	}
	return s, m
}
func TestJWTRevocationTTLAndRestart(t *testing.T) {
	s, m := testService(t)
	ctx := context.Background()
	raw, claims, err := s.Issue(ds.User{UserID: 7})
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := s.Issue(ds.User{UserID: 7})
	if raw == other {
		t.Fatal("each login needs a unique JWT")
	}
	if _, err = s.Validate(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if err = s.Logout(ctx, raw, claims); err != nil {
		t.Fatal(err)
	}
	key := BlacklistKey(raw)
	if !m.Exists(key) || m.TTL(key) <= 0 || m.TTL(key) > time.Hour || strings.Contains(key, raw) {
		t.Fatal("incorrect blacklist key/TTL")
	}
	// Recreate application service, keeping Redis intact.
	restarted, _ := New(secret, time.Hour, s.blacklist)
	if _, err = restarted.Validate(ctx, raw); !errors.Is(err, ErrRevokedToken) {
		t.Fatal("logout lost after restart")
	}
	if _, err = restarted.Validate(ctx, other); err != nil {
		t.Fatal("another login was revoked")
	}
	m.FastForward(time.Hour)
	if m.Exists(key) {
		t.Fatal("expired blacklist entry did not disappear")
	}
}
func TestJWTRejectsInvalidClaimsAndAlgorithms(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		claims *Claims
		key    string
	}{
		{"expired", jwt.SigningMethodHS256, &Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "1", ID: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Second))}, UserID: 1, Role: Creator}, secret},
		{"missing-exp", jwt.SigningMethodHS256, &Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "1", ID: "x"}, UserID: 1, Role: Creator}, secret},
		{"wrong-algorithm", jwt.SigningMethodHS384, &Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "1", ID: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, UserID: 1, Role: Creator}, secret},
		{"wrong-signature", jwt.SigningMethodHS256, &Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "1", ID: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, UserID: 1, Role: Creator}, "other"},
		{"wrong-user", jwt.SigningMethodHS256, &Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, Subject: "2", ID: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, UserID: 1, Role: Creator}, secret},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := jwt.NewWithClaims(tc.method, tc.claims).SignedString([]byte(tc.key))
			if _, err := s.Validate(ctx, raw); !errors.Is(err, ErrInvalidToken) {
				t.Fatal(err)
			}
		})
	}
}
func TestMiddlewareRolesAndBearerBoundary(t *testing.T) {
	s, m := testService(t)
	gin.SetMode(gin.TestMode)
	creator, _, _ := s.Issue(ds.User{UserID: 1})
	moderator, _, _ := s.Issue(ds.User{UserID: 2, IsModerator: true})
	r := gin.New()
	r.GET("/moderator", s.Middleware(true, Moderator), func(c *gin.Context) { c.Status(200) })
	for _, tc := range []struct {
		token string
		code  int
	}{{"", 401}, {creator, 403}, {moderator, 200}, {"bad", 401}} {
		req := httptest.NewRequest("GET", "/moderator", nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("got %d want %d", w.Code, tc.code)
		}
	}
	req := httptest.NewRequest("GET", "/moderator", nil)
	req.Header.Set("Cookie", "cloud_tariffs_jwt="+moderator)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("API accepted cookie without Bearer")
	}
	m.Close()
	req = httptest.NewRequest("GET", "/moderator", nil)
	req.Header.Set("Authorization", "Bearer "+moderator)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("Redis outage must fail closed: %d", w.Code)
	}
}

func TestPublicReadIgnoresInvalidExpiredAndRevokedTokens(t *testing.T) {
	s, m := testService(t)
	gin.SetMode(gin.TestMode)
	raw, claims, _ := s.Issue(ds.User{UserID: 1})
	if err := s.Logout(context.Background(), raw, claims); err != nil {
		t.Fatal(err)
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Second))
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	r := gin.New()
	r.GET("/public", s.Middleware(false), func(c *gin.Context) {
		if _, exists := c.Get("identity"); exists {
			t.Error("invalid identity set")
		}
		c.Status(200)
	})
	for _, header := range []string{"", "Bearer invalid", "Bearer " + expired, "Bearer " + raw, "Basic ignored"} {
		req := httptest.NewRequest("GET", "/public", nil)
		req.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("public read blocked: %d", w.Code)
		}
	}
	valid, _, _ := s.Issue(ds.User{UserID: 1})
	m.Close()
	req := httptest.NewRequest("GET", "/public", nil)
	req.Header.Set("Authorization", "Bearer "+valid)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("Redis outage blocked public data")
	}
}
