package models

import (
	"time"

	"github.com/google/uuid"
)

// Post groups one or more EntityPhoto rows (photos/videos) under a single
// shared description, the way a real Instagram post can carry several
// photos in one carousel with one caption — instead of the old model, where
// every uploaded photo was its own standalone "post" with its own caption.
//
// A Post always belongs to exactly one Entity (business) and is ordered
// within that business's feed by CreatedAt (newest first).
type Post struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	// Caption is optional, shared by every photo/video in this post.
	Caption string `json:"caption"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Photos are the one or more media items in this post, in display order.
	Photos []EntityPhoto `gorm:"foreignKey:PostID" json:"photos"`
}

func (Post) TableName() string {
	return "posts"
}
