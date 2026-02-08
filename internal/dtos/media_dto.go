package dtos

import "github.com/google/uuid"

// PhotoResponse is a simplified photo view.
type PhotoResponse struct {
	ID    uuid.UUID `json:"id"`
	URL   string    `json:"url"`
	Order int       `json:"order"`
}
