package handler

import (
	"errors"
	"strings"

	"cloud-tariffs-backend/internal/app/auth"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Credentials struct {
	Login    string `json:"login" example:"student"`
	Password string `json:"password" example:"student123"`
}
type TokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type" example:"Bearer"`
	ExpiresIn   int64     `json:"expires_in" example:"3600"`
	UserID      uint      `json:"user_id" example:"1"`
	Role        auth.Role `json:"role" example:"creator"`
}

// LoginAPI godoc
// @Summary Войти и получить JWT
// @Tags Пользователи
// @Produce json
// @Accept json
// @Param body body Credentials true "Логин и пароль"
// @Success 200 {object} TokenEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /users/login [post]
// @Failure 401 {object} ErrorEnvelope "Неверный логин или пароль"
func (h *Handler) LoginAPI(c *gin.Context) {
	if !noQuery(c) {
		return
	}
	var body Credentials
	if !strictJSON(c, &body) {
		return
	}
	body.Login = strings.ToLower(strings.TrimSpace(body.Login))
	if body.Login == "" || len(body.Login) > 50 || body.Password == "" || len(body.Password) > 72 {
		Fail(c, 400, "Нужны login и password (пароль не более 72 байт)")
		return
	}
	user, err := h.Repository.GetUserByLogin(body.Login)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Fail(c, 401, "Неверный логин или пароль")
		} else {
			apiError(c, err)
		}
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) != nil {
		Fail(c, 401, "Неверный логин или пароль")
		return
	}
	token, claims, err := h.Auth.Issue(user)
	if err != nil {
		apiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	success(c, 200, TokenResponse{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(h.Auth.TTL().Seconds()), UserID: user.UserID, Role: claims.Role})
}

// LogoutAPI godoc
// @Summary Выйти и отозвать текущий JWT
// @Tags Пользователи
// @Produce json
// @Security BearerAuth
// @Failure 401 {object} ErrorEnvelope "Нет JWT, токен истёк или отозван"
// @Failure 403 {object} ErrorEnvelope "Чужой тариф или недопустимая роль"
// @Failure 503 {object} ErrorEnvelope "Redis недоступен"
// @Success 200 {object} LogoutEnvelope
// @Failure 400 {object} ErrorEnvelope
// @Failure 404 {object} ErrorEnvelope
// @Failure 409 {object} ErrorEnvelope
// @Failure 500 {object} ErrorEnvelope
// @Router /users/logout [post]
func (h *Handler) LogoutAPI(c *gin.Context) {
	if !noQuery(c) || !noBody(c) {
		return
	}
	value, exists := c.Get("jwtClaims")
	claims, ok := value.(*auth.Claims)
	if !exists || !ok {
		Fail(c, 401, "Необходима авторизация")
		return
	}
	err := h.Auth.Logout(c.Request.Context(), c.GetString("jwt"), claims)
	if err != nil {
		Fail(c, 503, "Не удалось отозвать токен: Redis недоступен")
		return
	}
	success(c, 200, gin.H{"logged_out": true})
}
