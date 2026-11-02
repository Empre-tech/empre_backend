package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Message struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	SenderID     uuid.UUID `gorm:"type:uuid" json:"sender_id"`                                              // User who typed the message
	EntityID     uuid.UUID `gorm:"type:uuid;index:idx_conversation" json:"entity_id"`                       // Business
	UserID       uuid.UUID `gorm:"type:uuid;index:idx_conversation;index:idx_user_messages" json:"user_id"` // Customer
	SentByEntity bool      `json:"sent_by_entity"`                                                          // True if owner responding as business
	Content      string    `json:"content"`
	IsRead       bool      `gorm:"default:false" json:"is_read"`
	// Kind is "text" (default) or "appointment": a card for an appointment
	// shown inside the chat thread. AppointmentID points to it.
	Kind          string         `gorm:"type:varchar(20);default:'text'" json:"kind"`
	AppointmentID *uuid.UUID     `gorm:"type:uuid;index" json:"appointment_id,omitempty"`
	// ItemID: el producto o servicio del catálogo por el que se pregunta (opcional).
	// Item es una copia de cómo se veía en ese momento (nombre, precio, foto), para
	// que la tarjeta del chat siga diciendo lo mismo aunque luego lo cambien o lo quiten.
	ItemID *uuid.UUID   `gorm:"type:uuid;index" json:"item_id,omitempty"`
	Item   *MessageItem `gorm:"type:jsonb;serializer:json" json:"item,omitempty"`
	CreatedAt     time.Time      `gorm:"index:idx_conversation,priority:3" json:"created_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index:idx_conversation,priority:4" json:"-"`

	// Associations
	Entity Entity `gorm:"foreignKey:EntityID" json:"entity,omitempty"`
	User   User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// MessageItem es la tarjeta de un producto o servicio dentro de un mensaje.
type MessageItem struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Price     *int      `json:"price"`
	PriceFrom bool      `json:"price_from"`
	// Photo: id de la primera foto (la app arma la URL); vacío si no tiene.
	Photo   string `json:"photo,omitempty"`
	Section string `json:"section,omitempty"`
	Kind    string `json:"kind,omitempty"`
}
