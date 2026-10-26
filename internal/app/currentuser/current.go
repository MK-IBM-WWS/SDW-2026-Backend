// Package currentuser reads the authenticated identity from each Gin request.
package currentuser

import "github.com/gin-gonic/gin"

// ЛР4. ID и роль пользователя относятся к одному конкретному запросу.
// Здесь больше нет постоянного пользователя с ID=1 для всех клиентов.
type Identity struct {
	UserID uint
	Role   string
}

// Читаем личность, которую middleware уже получил из проверенного JWT.
func Get(c *gin.Context) Identity {
	value, exists := c.Get("identity")
	// Если JWT не предоставлен или не принят на публичном маршруте, возвращаем гостя.
	// Пустая Identity имеет UserID=0; это признак отсутствия вошедшего пользователя.
	if !exists {
		return Identity{}
	}
	// Берём значение нужного типа. Если тип другой, результат будет пустой Identity.
	identity, _ := value.(Identity)
	return identity
}

// Удобный способ передать ID текущего пользователя в запрос к данным.
func (i Identity) ID() uint { return i.UserID }
