package dtos

import (
	"empre_backend/internal/models"
	"time"

	"github.com/google/uuid"
)

// UserResponse is the standard exposure of a User.
type UserResponse struct {
	ID                uuid.UUID   `json:"id"`
	Name              string      `json:"name"`
	Email             string      `json:"email"`
	Phone             string      `json:"phone,omitempty"`
	ProfilePictureURL string      `json:"profile_picture_url"`
	Role              models.Role `json:"role"`
}

// EntityMapDTO is a lightweight version for listing on maps.
type EntityMapDTO struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	CategoryName string    `json:"category_name"`
	ProfileURL   string    `json:"profile_url"`
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	IsVerified   bool      `json:"is_verified"`
}

// EntityDetailDTO is the full view for a single entity page.
type EntityDetailDTO struct {
	ID                 uuid.UUID                 `json:"id"`
	Name               string                    `json:"name"`
	Description        string                    `json:"description"`
	Category           models.Category           `json:"category"`
	Address            string                    `json:"address"`
	City               string                    `json:"city"`
	ContactInfo        string                    `json:"contact_info"`
	BannerURL          string                    `json:"banner_url"`
	ProfileURL         string                    `json:"profile_url"`
	Latitude           float64                   `json:"latitude"`
	Longitude          float64                   `json:"longitude"`
	VerificationStatus models.VerificationStatus `json:"verification_status"`
	IsVerified         bool                      `json:"is_verified"`
	OwnerID            uuid.UUID                 `json:"owner_id"`
	CreatedAt          time.Time                 `json:"created_at"`

	// Gallery
	Photos []models.EntityPhoto `json:"photos,omitempty"`
}
