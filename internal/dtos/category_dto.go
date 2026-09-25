package dtos

import "github.com/google/uuid"

// SubcategoryResponse is a lightweight subcategory view.
type SubcategoryResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	CategoryID uuid.UUID `json:"category_id"`
}

// CategoryResponse is a lightweight category view. Subcategories is only
// populated by endpoints that explicitly load it (e.g. GET /api/categories).
type CategoryResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Icon is an Ionicons name (e.g. "restaurant-outline"), used for the
	// category's own badge and for the marker of its businesses on the map.
	Icon          string                 `json:"icon"`
	Subcategories []SubcategoryResponse `json:"subcategories,omitempty"`
}
