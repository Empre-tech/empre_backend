package models

import (
	"time"

	"github.com/google/uuid"
)

// Favorite marks that a user saved a business as a favorite. One row per
// (user, entity) pair; un-favoriting deletes the row outright (no history
// needed here).
type Favorite struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_favorite_user_entity" json:"user_id"`
	EntityID  uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_favorite_user_entity" json:"entity_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (Favorite) TableName() string {
	return "favorites"
}
