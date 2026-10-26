package handler

import "cloud-tariffs-backend/internal/app/ds"

// Response DTOs document the status/data envelope used by the API.
// Ошибка: status=fail и сообщение, почему запрос не выполнен.
type ErrorEnvelope struct {
	Status  string `json:"status" example:"fail"`
	Message string `json:"message"`
}

// Успешный ответ с одним тарифом. Указатель позволяет вернуть data:null при отсутствии черновика или пустой ленте.
type TariffEnvelope struct {
	Status string          `json:"status" example:"success"`
	Data   *TariffResponse `json:"data"`
}

// Успешный ответ со списком тарифов в data.
type ListEnvelope struct {
	Status string               `json:"status" example:"success"`
	Data   []ListTariffResponse `json:"data"`
}

// Ответ регистрации с пользователем; модель User не сериализует хеш пароля.
type UserEnvelope struct {
	Status string  `json:"status" example:"success"`
	Data   ds.User `json:"data"`
}

// Ответ входа: JWT и сведения об аккаунте в data.
type TokenEnvelope struct {
	Status string        `json:"status" example:"success"`
	Data   TokenResponse `json:"data"`
}

// Признак того, что логическое удаление выполнено.
type DeleteData struct {
	Deleted bool `json:"deleted"`
}

// Общая оболочка ответа удаления: status и data.
type DeleteEnvelope struct {
	Status string     `json:"status" example:"success"`
	Data   DeleteData `json:"data"`
}

// Ответ лайка: ID тарифа, личный признак 0/1 и общее число лайков.
type LikeData struct {
	TariffID  uint  `json:"tariff_id"`
	IsLiked   int   `json:"is_liked"`
	LikeCount int64 `json:"like_count"`
}

// Общая оболочка ответа лайка: status и data.
type LikeEnvelope struct {
	Status string   `json:"status" example:"success"`
	Data   LikeData `json:"data"`
}

// logged_out=true подтверждает успешный отзыв текущего JWT.
type LogoutData struct {
	LoggedOut bool `json:"logged_out"`
}

// Общая оболочка ответа выхода: status и data.
type LogoutEnvelope struct {
	Status string     `json:"status" example:"success"`
	Data   LogoutData `json:"data"`
}
