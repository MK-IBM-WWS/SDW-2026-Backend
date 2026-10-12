package handler

import "cloud-tariffs-backend/internal/app/ds"

// Response DTOs document the status/data envelope used by the API.
type ErrorEnvelope struct {
	Status  string `json:"status" example:"fail"`
	Message string `json:"message"`
}
type TariffEnvelope struct {
	Status string          `json:"status" example:"success"`
	Data   *TariffResponse `json:"data"`
}
type ListEnvelope struct {
	Status string           `json:"status" example:"success"`
	Data   []TariffResponse `json:"data"`
}
type UserEnvelope struct {
	Status string  `json:"status" example:"success"`
	Data   ds.User `json:"data"`
}
type TokenEnvelope struct {
	Status string        `json:"status" example:"success"`
	Data   TokenResponse `json:"data"`
}
type DeleteData struct {
	Deleted bool `json:"deleted"`
}
type DeleteEnvelope struct {
	Status string     `json:"status" example:"success"`
	Data   DeleteData `json:"data"`
}
type LikeData struct {
	TariffID  uint  `json:"tariff_id"`
	IsLiked   int   `json:"is_liked"`
	LikeCount int64 `json:"like_count"`
}
type LikeEnvelope struct {
	Status string   `json:"status" example:"success"`
	Data   LikeData `json:"data"`
}
type LogoutData struct {
	LoggedOut bool `json:"logged_out"`
}
type LogoutEnvelope struct {
	Status string     `json:"status" example:"success"`
	Data   LogoutData `json:"data"`
}
