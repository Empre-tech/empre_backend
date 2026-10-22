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
	// Verified: la reseña está respaldada por un canje confirmado.
	Verified bool `json:"verified"`
}

// ReviewSummary is the aggregate rating shown on a business profile.
type ReviewSummary struct {
	Average float64 `json:"average"`
	Count   int64   `json:"count"`
	// Distribution: reseñas por calificación; índice 0 = 1 estrella … índice 4 = 5.
	Distribution [5]int64 `json:"distribution"`
	// VerifiedCount: cuántas reseñas están respaldadas por un canje confirmado.
	VerifiedCount int64 `json:"verified_count"`
}

// ReviewSnippetResponse es la frase de reseña de un destacado: solo reseñas verificadas, autor abreviado.
type ReviewSnippetResponse struct {
	Text      string    `json:"text"`
	Rating    int       `json:"rating"`
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author"`
	Verified  bool      `json:"verified"`
}
