package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PromotionStatus es el ciclo de vida de una promoción (editable).
type PromotionStatus string

const (
	PromotionActive   PromotionStatus = "active"
	PromotionPaused   PromotionStatus = "paused"
	PromotionFinished PromotionStatus = "finished"
)

func (s PromotionStatus) Valid() bool {
	switch s {
	case PromotionActive, PromotionPaused, PromotionFinished:
		return true
	}
	return false
}

// Promotion es una oferta sostenida (días o semanas), sin cupos ni reclamo/QR.
// A diferencia del relámpago (Offer), es editable y se puede pausar o
// finalizar. El cliente la muestra o la menciona; "Guardar" es opcional.
type Promotion struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`

	Title       string `gorm:"not null" json:"title"`
	Description string `json:"description"`
	// Precio o porcentaje de descuento (al menos uno); valor normal opcional.
	Price       string `json:"price"`
	Discount    string `json:"discount"`
	NormalPrice string `json:"normal_price"`

	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `gorm:"index" json:"ends_at"`
	Conditions string    `json:"conditions"`

	// Foto opcional (Media). Queda preparada para cuando se conecte la subida.
	PhotoMediaID *uuid.UUID `gorm:"type:uuid" json:"photo_media_id,omitempty"`
	// Etiquetas (producto/servicio) separadas por coma; alimentan la búsqueda.
	Tags   string          `json:"tags"`
	Status PromotionStatus `gorm:"type:varchar(20);default:'active'" json:"status"`

	// Entidad dueña (para distancia y categoría en el listado de ofertas).
	Entity Entity `gorm:"foreignKey:EntityID" json:"-"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
