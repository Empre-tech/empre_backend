package models

import (
	"github.com/google/uuid"
)

// EntityPhoto is one photo or video. It always belongs to a Post (the shared
// caption lives on the Post, not here anymore — see post.go) and, denormalized,
// also keeps EntityID so queries that just need "every photo of this business,
// flat" (like the profile grid) don't need to join through posts.
type EntityPhoto struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	PostID   uuid.UUID `gorm:"type:uuid;not null;index" json:"post_id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	MediaID  uuid.UUID `gorm:"type:uuid;not null" json:"media_id"`
	// Order within its Post (for the carousel) and, transitively, within the grid.
	Order int `gorm:"default:0" json:"order"`

	// Associations
	Media Media `gorm:"foreignKey:MediaID" json:"media"`
	Post  Post  `gorm:"foreignKey:PostID" json:"-"`
}
