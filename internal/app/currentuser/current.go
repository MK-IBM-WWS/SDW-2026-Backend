package currentuser

import "sync"

const CreatorID uint = 1

type Identity struct{ id uint }

var once sync.Once
var instance *Identity

func Get() *Identity {
	once.Do(func() { instance = &Identity{id: CreatorID} })
	return instance
}
func (i *Identity) ID() uint { return i.id }
