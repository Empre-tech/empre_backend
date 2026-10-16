package models

import (
	"time"

	"github.com/google/uuid"
)

// PromotionItem es un servicio del catálogo que una promoción destaca, CONGELADO al publicarla (nombre y precio
// habitual de ese momento): cambiar o retirar el servicio después no altera una campaña ya publicada. Se vuelve a
// congelar solo si el dueño edita los servicios de la promoción.
type PromotionItem struct {
	ID            uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	PromotionID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"promotion_id"`
	ServiceItemID *uuid.UUID `gorm:"type:uuid" json:"service_item_id,omitempty"`
	Name          string     `gorm:"size:80;not null" json:"name"`
	// Price es el precio habitual al congelar; nil = el servicio no tenía precio publicado.
	Price     *int      `json:"price"`
	Position  int       `gorm:"default:0" json:"position"`
	CreatedAt time.Time `json:"created_at"`
}

func (PromotionItem) TableName() string {
	return "promotion_items"
}
