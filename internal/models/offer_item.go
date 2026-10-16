package models

import (
	"time"

	"github.com/google/uuid"
)

// OfferItem es un servicio del catálogo incluido en una oferta, CONGELADO al publicarla: guarda el nombre y el precio
// habitual que tenía en ese momento. Así, cambiar o retirar el servicio del catálogo después no altera una oferta ya
// publicada, y la oferta nunca escribe en el catálogo. No se actualiza.
type OfferItem struct {
	ID      uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OfferID uuid.UUID `gorm:"type:uuid;not null;index" json:"offer_id"`
	// ServiceItemID apunta al servicio original (para analítica); puede quedar huérfano si se retira.
	ServiceItemID *uuid.UUID `gorm:"type:uuid" json:"service_item_id,omitempty"`
	Name          string     `gorm:"size:80;not null" json:"name"`
	// Price es el precio habitual al publicar; nil = el servicio no tenía precio publicado.
	Price     *int      `json:"price"`
	Position  int       `gorm:"default:0" json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

func (OfferItem) TableName() string {
	return "offer_items"
}
