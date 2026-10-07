package dtos

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// OfferResponse is the public shape of an offer.
type OfferResponse struct {
	ID           uuid.UUID `json:"id"`
	EntityID     uuid.UUID `json:"entity_id"`
	Type         string    `json:"type"`
	Title        string    `json:"title"`
	Price        string    `json:"price"`
	NormalPrice  string    `json:"normal_price,omitempty"`
	Conditions   string    `json:"conditions,omitempty"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	TotalSlots   int       `json:"total_slots"`
	UsedSlots    int       `json:"used_slots"`
	SlotsLeft    int       `json:"slots_left"`
	RadiusMeters int       `json:"radius_meters"`
	RepeatPolicy string    `json:"repeat_policy"`
}

// OfferResponseFrom converts a model offer to its response shape.
func OfferResponseFrom(o *models.Offer) OfferResponse {
	slotsLeft := o.TotalSlots - o.UsedSlots
	if slotsLeft < 0 {
		slotsLeft = 0
	}
	return OfferResponse{
		ID:           o.ID,
		EntityID:     o.EntityID,
		Type:         string(o.Type),
		Title:        o.Title,
		Price:        o.Price,
		NormalPrice:  o.NormalPrice,
		Conditions:   o.Conditions,
		StartsAt:     o.StartsAt,
		EndsAt:       o.EndsAt,
		TotalSlots:   o.TotalSlots,
		UsedSlots:    o.UsedSlots,
		SlotsLeft:    slotsLeft,
		RadiusMeters: o.RadiusMeters,
		RepeatPolicy: string(o.RepeatPolicy),
	}
}
