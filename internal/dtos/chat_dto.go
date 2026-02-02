package dtos

import (
	"time"

	"github.com/google/uuid"
)

// ConversationResponse represents a summarized chat item for list views.
type ConversationResponse struct {
	ID           uuid.UUID       `json:"id"`             // Message ID
	Content      string          `json:"content"`        // Latest message snippet
	CreatedAt    time.Time       `json:"created_at"`     // Latest message time
	IsRead       bool            `json:"is_read"`        // Read status
	SentByEntity bool            `json:"sent_by_entity"` // True if sent by the business
	OtherParty   OtherPartyStats `json:"other_party"`    // The other person/entity in the chat
}

// MessageResponse represents a detailed message in a conversation history.
type MessageResponse struct {
	ID           uuid.UUID `json:"id"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
	IsRead       bool      `json:"is_read"`
	SentByEntity bool      `json:"sent_by_entity"`
	SenderID     uuid.UUID `json:"sender_id"` // Included for context in group chats (future proofing)
}

// OtherPartyStats contains minimal info about the chat partner.
// Used to render the list item (Name, Photo).
type OtherPartyStats struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	ProfileURL string    `json:"profile_url"`
	Type       string    `json:"type"` // "user" or "entity" to help frontend routing
}
