package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Message struct {
	ID             uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	ConversationID uuid.UUID      `gorm:"type:uuid;index" json:"conversation_id"`
	SenderID       uuid.UUID      `gorm:"type:uuid" json:"sender_id"`       // User who typed the message
	UserID         uuid.UUID      `gorm:"type:uuid;index" json:"user_id"`   // Customer
	EntityID       uuid.UUID      `gorm:"type:uuid;index" json:"entity_id"` // Business
	SentByEntity   bool           `json:"sent_by_entity"`                   // True if owner responding as business
	Content        string         `json:"content"`
	IsRead         bool           `gorm:"default:false" json:"is_read"`
	CreatedAt      time.Time      `json:"created_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`

	// Associations
	Entity Entity `gorm:"foreignKey:EntityID" json:"entity,omitempty"`
	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
