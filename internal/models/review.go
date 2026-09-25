package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Review is one user's rating + optional comment on a business (Entity). A
// user can have at most one review per entity (enforced by a unique index);
// writing again updates the existing row instead of creating a new one.
type Review struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID  uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:idx_review_entity_user" json:"entity_id"`
	UserID    uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:idx_review_entity_user" json:"user_id"`
	Rating    int            `gorm:"not null" json:"rating"`
	Comment   string         `json:"comment"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (Review) TableName() string {
	return "reviews"
}
