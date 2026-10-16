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
	// ClaimWindowMinutes: cuánto dura un reclamo de esta oferta antes de expirar.
	ClaimWindowMinutes int `json:"claim_window_minutes"`
	// Precio temporal propio de la oferta y referencia congelada (precio habitual de lo incluido). Solo hay
	// ahorro cuando existen los dos y el temporal es menor: nunca un descuento sin referencia válida.
	SpecialPrice   *int `json:"special_price,omitempty"`
	ReferencePrice *int `json:"reference_price,omitempty"`
	Saving         *int `json:"saving,omitempty"`
	// Items son los servicios del catálogo incluidos, tal como estaban al publicar.
	Items []OfferItemResponse `json:"items,omitempty"`
}

// OfferItemResponse es un servicio incluido en una oferta (copia congelada al publicar).
type OfferItemResponse struct {
	Name  string `json:"name"`
	Price *int   `json:"price,omitempty"`
}

// OfferSaving es lo que se ahorra: precio de referencia menos precio especial. Nil si falta alguno o si el especial
// no es menor.
func OfferSaving(special, reference *int) *int {
	if special == nil || reference == nil || *special >= *reference {
		return nil
	}
	s := *reference - *special
	return &s
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

		ClaimWindowMinutes: int(o.Type.ClaimWindow().Minutes()),
		SpecialPrice:       o.SpecialPrice,
		ReferencePrice:     o.ReferencePrice,
		Saving:             OfferSaving(o.SpecialPrice, o.ReferencePrice),
		Items:              offerItems(o.Items),
	}
}

func offerItems(items []models.OfferItem) []OfferItemResponse {
	if len(items) == 0 {
		return nil
	}
	out := make([]OfferItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, OfferItemResponse{Name: it.Name, Price: it.Price})
	}
	return out
}
