package dtos

import (
	"time"

	"github.com/google/uuid"
)

// ReviewUser is the minimal author info shown alongside a review.
type ReviewUser struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	ProfilePictureURL string    `json:"profile_picture_url"`
}

// ReviewResponse is one review as shown in a business's review list.
type ReviewResponse struct {
	ID        uuid.UUID  `json:"id"`
	Rating    int        `json:"rating"`
	Comment   string     `json:"comment"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	User      ReviewUser `json:"user"`
}

// ReviewSummary is the aggregate rating shown on a business profile.
type ReviewSummary struct {
	Average float64 `json:"average"`
	Count   int64   `json:"count"`
}
