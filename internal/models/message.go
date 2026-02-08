package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Message struct {
	ID           uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	SenderID     uuid.UUID      `gorm:"type:uuid" json:"sender_id"`                                              // User who typed the message
	EntityID     uuid.UUID      `gorm:"type:uuid;index:idx_conversation" json:"entity_id"`                       // Business
	UserID       uuid.UUID      `gorm:"type:uuid;index:idx_conversation;index:idx_user_messages" json:"user_id"` // Customer
	SentByEntity bool           `json:"sent_by_entity"`                                                          // True if owner responding as business
	Content      string         `json:"content"`
	IsRead       bool           `gorm:"default:false" json:"is_read"`
	CreatedAt    time.Time      `gorm:"index:idx_conversation,priority:3" json:"created_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index:idx_conversation,priority:4" json:"-"`

	// Associations
	Entity Entity `gorm:"foreignKey:EntityID" json:"entity,omitempty"`
	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
