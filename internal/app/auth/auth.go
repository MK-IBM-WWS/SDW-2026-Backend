// Package auth implements JWT authentication and revocation in Redis.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cloud-tariffs-backend/internal/app/currentuser"
	"cloud-tariffs-backend/internal/app/ds"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Role string

const (
	Creator   Role = "creator"
	Moderator Role = "moderator"
	issuer         = "cloud-tariffs"
)

var ErrInvalidToken = errors.New("недействительный токен")
var ErrRevokedToken = errors.New("токен отозван")
var ErrBlacklistUnavailable = errors.New("Redis blacklist недоступен")

type Claims struct {
	jwt.RegisteredClaims
	UserID uint `json:"user_id"`
	Role   Role `json:"role"`
}

type Blacklist interface {
	Exists(context.Context, string) (bool, error)
	Revoke(context.Context, string, time.Duration) error
}

type RedisBlacklist struct{ Client *redis.Client }

func BlacklistKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	return "cloud-tariffs:jwt:blacklist:" + hex.EncodeToString(hash[:])
}
func (b RedisBlacklist) Exists(ctx context.Context, token string) (bool, error) {
	n, err := b.Client.Exists(ctx, BlacklistKey(token)).Result()
	return n > 0, err
}
func (b RedisBlacklist) Revoke(ctx context.Context, token string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	return b.Client.Set(ctx, BlacklistKey(token), "1", ttl).Err()
}

type Service struct {
	secret    []byte
	ttl       time.Duration
	blacklist Blacklist
}

func New(secret string, ttl time.Duration, blacklist Blacklist) (*Service, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET должен содержать не менее 32 байт")
	}
	if ttl < time.Second || ttl > 24*time.Hour {
		return nil, fmt.Errorf("JWT_EXPIRES_IN: от 1s до 24h")
	}
	if blacklist == nil {
		return nil, fmt.Errorf("blacklist не настроен")
	}
	return &Service{secret: []byte(secret), ttl: ttl, blacklist: blacklist}, nil
}
func (s *Service) TTL() time.Duration { return s.ttl }
func (s *Service) Issue(user ds.User) (string, *Claims, error) {
	role := Creator
	if user.IsModerator {
		role = Moderator
	}
	now := time.Now()
	claims := &Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: issuer, Subject: strconv.FormatUint(uint64(user.UserID), 10), ID: uuid.NewString(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}, UserID: user.UserID, Role: role}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	return token, claims, err
}
func (s *Service) Validate(ctx context.Context, raw string) (*Claims, error) {
	if raw == "" || len(raw) > 4096 {
		return nil, ErrInvalidToken
	}
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired(), jwt.WithIssuer(issuer), jwt.WithIssuedAt())
	if err != nil || !token.Valid || claims.UserID == 0 || claims.ID == "" ||
		claims.Subject != strconv.FormatUint(uint64(claims.UserID), 10) || (claims.Role != Creator && claims.Role != Moderator) {
		return nil, ErrInvalidToken
	}
	revoked, err := s.blacklist.Exists(ctx, raw)
	if err != nil {
		return nil, ErrBlacklistUnavailable
	}
	if revoked {
		return nil, ErrRevokedToken
	}
	return claims, nil
}
func (s *Service) Logout(ctx context.Context, raw string, claims *Claims) error {
	// Keep the blacklist entry only until this token's actual expiration.
	if err := s.blacklist.Revoke(ctx, raw, time.Until(claims.ExpiresAt.Time)); err != nil {
		return ErrBlacklistUnavailable
	}
	return nil
}
func fail(c *gin.Context, code int, message string) {
	if code == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", "Bearer")
	}
	c.AbortWithStatusJSON(code, gin.H{"status": "fail", "message": message})
}

// Middleware requires a Bearer JWT for private methods. Public reads use a
// valid JWT for personalization and otherwise continue as an anonymous visitor.
func (s *Service) Middleware(required bool, roles ...Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			if required {
				fail(c, 401, "Необходим Authorization: Bearer <JWT>")
			} else {
				c.Next()
			}
			return
		}
		raw := parts[1]
		claims, err := s.Validate(c.Request.Context(), raw)
		if err != nil {
			if !required {
				c.Next()
				return
			}
			if errors.Is(err, ErrBlacklistUnavailable) {
				fail(c, 503, "Redis недоступен; проверка авторизации временно невозможна")
			} else {
				fail(c, 401, err.Error())
			}
			return
		}
		if len(roles) > 0 {
			allowed := false
			for _, role := range roles {
				if role == claims.Role {
					allowed = true
					break
				}
			}
			if !allowed {
				fail(c, 403, "Роль не имеет доступа к этому методу")
				return
			}
		}
		c.Set("identity", currentuser.Identity{UserID: claims.UserID, Role: string(claims.Role)})
		c.Set("jwt", raw)
		c.Set("jwtClaims", claims)
		c.Next()
	}
}
