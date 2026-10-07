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

	// Vigencia.
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `gorm:"index" json:"ends_at"`

	// Slots.
	TotalSlots int `gorm:"not null" json:"total_slots"`
	UsedSlots  int `gorm:"default:0" json:"used_slots"`

	// Visibility radius in meters (default 500, per product decision).
	RadiusMeters int `gorm:"default:500" json:"radius_meters"`

	RepeatPolicy RepeatPolicy `gorm:"type:varchar(20);default:'once'" json:"repeat_policy"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
