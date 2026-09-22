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
	ID            uuid.UUID             `json:"id"`
	Name          string                `json:"name"`
	Subcategories []SubcategoryResponse `json:"subcategories,omitempty"`
}
