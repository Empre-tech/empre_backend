package dtos

import "github.com/google/uuid"

// SearchBusiness es un negocio en la respuesta de búsqueda por niveles.
type SearchBusiness struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	CategoryID     uuid.UUID `json:"category_id"`
	CategoryName   string    `json:"category_name"`
	CategoryIcon   string    `json:"category_icon"`
	CategoryColor  string    `json:"category_color,omitempty"`
	ProfileURL     string    `json:"profile_url"`
	Latitude       float64   `json:"latitude"`
	Longitude      float64   `json:"longitude"`
	IsOpenNow      bool      `json:"is_open_now"`
	HasHours       bool      `json:"has_hours"`
	HasActiveOffer bool      `json:"has_active_offer"`
	DistanceKM     float64   `json:"distance_km"`
}

// NearestBusiness es el negocio más cercano fuera del viewport (nivel 2).
type NearestBusiness struct {
	Business   SearchBusiness `json:"business"`
	DistanceKM float64        `json:"distance_km"`
}

// SearchResponse es la respuesta por niveles del endpoint de búsqueda.
type SearchResponse struct {
	// Nivel 1: coincidencias en el viewport (categoría o texto).
	Exact []SearchBusiness `json:"exact"`
	// Nivel 2: el más cercano fuera del viewport (con su distancia).
	NearestBeyond *NearestBusiness `json:"nearest_beyond"`
	// Nivel 3: categorías relacionadas cercanas (máx 3).
	Related []SearchBusiness `json:"related"`
	// Nivel 4: relámpagos cercanos de categorías relacionadas.
	Offers []RelampagoListItem `json:"offers"`
	// Count es la cantidad de resultados de la consulta (no de marcadores).
	Count int `json:"count"`
}

// CategoryChip es una categoría para los chips dinámicos.
type CategoryChip struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Icon  string    `json:"icon"`
	Color string    `json:"color,omitempty"`
	Count int64     `json:"count"`
}
