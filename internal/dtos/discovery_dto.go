package dtos

import (
	"time"

	"github.com/google/uuid"
)

// DiscoveryDTO is one ranked discovery result: the map card fields plus the
// computed distance and any active offer.
type DiscoveryDTO struct {
	EntityMapDTO
	// Subcategorías y frase corta: para «También podría interesarte» y las tarjetas.
	SubcategoryIDs []uuid.UUID    `json:"subcategory_ids,omitempty"`
	Subcategory    string         `json:"subcategory,omitempty"`
	Tagline        string         `json:"tagline,omitempty"`
	DistanceMeters float64        `json:"distance_meters"`
	HasActiveOffer bool           `json:"has_active_offer"`
	ActiveOffer    *OfferResponse `json:"active_offer,omitempty"`
}

// DiscoverSectionDTO es una sección de «Descubre», ya armada por el servidor.
type DiscoverSectionDTO struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Items []DiscoverItemDTO `json:"items"`
}

// DiscoverItemDTO es la tarjeta: lo justo para dibujarla y una sola razón.
type DiscoverItemDTO struct {
	EntityID       uuid.UUID         `json:"entity_id"`
	Name           string            `json:"name"`
	CategoryName   string            `json:"category_name"`
	CategoryIcon   string            `json:"category_icon"`
	CategoryColor  string            `json:"category_color,omitempty"`
	Subcategory    string            `json:"subcategory,omitempty"`
	Latitude       float64           `json:"latitude"`
	Longitude      float64           `json:"longitude"`
	DistanceMeters float64           `json:"distance_meters"`
	IsOpenNow      bool              `json:"is_open_now"`
	ClosesAt       string            `json:"closes_at,omitempty"`
	CoverURL       string            `json:"cover_url,omitempty"`
	ProfileURL     string            `json:"profile_url,omitempty"`
	Tagline        string            `json:"tagline,omitempty"`
	Reason         DiscoverReasonDTO `json:"reason"`
}

type DiscoverReasonDTO struct {
	Kind       string     `json:"kind"`
	EndsAt     *time.Time `json:"ends_at,omitempty"`
	PostedAt   *time.Time `json:"posted_at,omitempty"`
	OfferTitle string     `json:"offer_title,omitempty"`
}
