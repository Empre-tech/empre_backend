package dtos

import "github.com/google/uuid"

// CategoryResponse is a lightweight category view.
type CategoryResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
