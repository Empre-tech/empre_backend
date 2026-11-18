package models

import (
	"time"

	"github.com/google/uuid"
)

// AIWrite es un pedido a «Redactar con Empre AI» (2026-11-17): cuenta en el mismo tope del mes que «Habla con Empre
// AI». Sin EntityID es la primera descripción al crear el negocio, que es gratis con un tope diario por persona.
type AIWrite struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	EntityID  *uuid.UUID `gorm:"type:uuid;index:idx_ai_writes_entity,priority:1" json:"entity_id,omitempty"`
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index:idx_ai_writes_user,priority:1" json:"user_id"`
	Kind      string     `gorm:"size:32;not null" json:"kind"`
	CreatedAt time.Time  `gorm:"not null;index:idx_ai_writes_entity,priority:2;index:idx_ai_writes_user,priority:2" json:"created_at"`
}

func (AIWrite) TableName() string { return "ai_writes" }
