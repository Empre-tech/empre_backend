package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OfferType is the single discriminator for the three offer kinds. The rules
// for each type (duration, slots, expiration, repetition, active limits) live
// as configuration in the service layer, not as three separate tables, so a
// fourth type can be added without redesigning the schema.
type OfferType string

const (
	OfferHours OfferType = "HORAS"
	OfferDay   OfferType = "DIA"
	OfferWeek  OfferType = "SEMANA"
)

func (t OfferType) Valid() bool {
	switch t {
	case OfferHours, OfferDay, OfferWeek:
		return true
	}
	return false
}

// ClaimWindow es cuánto dura un reclamo antes de expirar (Backend v2 §4.2):
// 30 min en las ofertas por horas, 3 h en las del día y 24 h en las de la
// semana. Es la única fuente: el servicio la usa para calcular el vencimiento y
// la oferta la publica para que la app muestre la regla antes de reclamar.
func (t OfferType) ClaimWindow() time.Duration {
	switch t {
	case OfferDay:
		return 3 * time.Hour
	case OfferWeek:
		return 24 * time.Hour
	default:
		return 30 * time.Minute
	}
}

// RepeatPolicy controls how many times a user can claim the same offer.
type RepeatPolicy string

const (
	RepeatOnce  RepeatPolicy = "once"  // once per user per offer
	RepeatDaily RepeatPolicy = "daily" // once per user per day
)

// Offer is an immutable publication by a business. Title, price and
// conditions never change after creation: quick actions (extend, widen
// radius, add slots) only touch vigencia/radius/slots, and "volver a
// publicar" clones the offer into a brand-new row instead of editing it.
type Offer struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	Type     OfferType `gorm:"type:varchar(20);not null" json:"type"`

	// Immutable content.
	Title      string `gorm:"not null" json:"title"`
	Price      string `json:"price"`
	// NormalPrice es el valor habitual (opcional); si está, se muestra el ahorro.
	NormalPrice string `json:"normal_price"`
	Conditions  string `json:"conditions"`
	// OwnerNote es la nota corta de quien atiende, con su voz («Hoy hay sancocho, llega antes de la 1»). La muestran la
	// tarjeta y la historia del negocio en Ofertas (3.48). Opcional.
	OwnerNote string `gorm:"size:90" json:"owner_note"`

	// Vigencia.
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `gorm:"index" json:"ends_at"`

	// Slots.
	TotalSlots int `gorm:"not null" json:"total_slots"`
	UsedSlots  int `gorm:"default:0" json:"used_slots"`

	// Visibility radius in meters (default 500, per product decision).
	RadiusMeters int `gorm:"default:500" json:"radius_meters"`

	RepeatPolicy RepeatPolicy `gorm:"type:varchar(20);default:'once'" json:"repeat_policy"`

	// SpecialPrice es el precio TEMPORAL de la oferta (pesos). Es propio de la oferta: nunca escribe el precio
	// habitual del catálogo. La base lo exige entre 0 y 10.000.000 y, si hay referencia, menor que ella.
	SpecialPrice *int `gorm:"check:chk_offers_special_price,special_price IS NULL OR (special_price >= 0 AND special_price <= 10000000 AND (reference_price IS NULL OR special_price < reference_price))" json:"special_price"`
	// ReferencePrice es la suma del precio habitual de los servicios incluidos, CONGELADA al publicar. Solo existe
	// si todos los servicios tenían precio: sin ella no se calcula ni se muestra ningún ahorro.
	ReferencePrice *int `gorm:"check:chk_offers_reference_price,reference_price IS NULL OR reference_price > 0" json:"reference_price"`
	// Items son los servicios del catálogo incluidos, congelados al publicar.
	Items []OfferItem `gorm:"foreignKey:OfferID" json:"-"`

	// Entidad dueña (para distancia y categoría en el listado de ofertas).
	Entity Entity `gorm:"foreignKey:EntityID" json:"-"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
