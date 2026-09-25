package models

import (
	"time"

	"github.com/google/uuid"
)

// PushToken is an Expo push token registered for a device. A user can have
// several (one per device); the same token is only ever tied to one user at
// a time, so logging in as someone else on a shared device reassigns it
// instead of leaving a stale row that would notify the wrong person.
type PushToken struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	Token     string    `gorm:"not null;uniqueIndex" json:"token"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (PushToken) TableName() string {
	return "push_tokens"
}
