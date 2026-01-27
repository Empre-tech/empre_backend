package models

import (
	"github.com/google/uuid"
)

type EntityPhoto struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	MediaID  uuid.UUID `gorm:"type:uuid;not null" json:"media_id"`
	Order    int       `gorm:"default:0" json:"order"`

	// Associations
	Media Media `gorm:"foreignKey:MediaID" json:"media"`
}
