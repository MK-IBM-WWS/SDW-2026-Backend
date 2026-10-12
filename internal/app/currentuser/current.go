// Package currentuser reads the authenticated identity from each Gin request.
package currentuser

import "github.com/gin-gonic/gin"

type Identity struct {
	UserID uint
	Role   string
}

func Get(c *gin.Context) Identity {
	value, exists := c.Get("identity")
	if !exists {
		return Identity{}
	}
	identity, _ := value.(Identity)
	return identity
}
func (i Identity) ID() uint { return i.UserID }
