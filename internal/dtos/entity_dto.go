package dtos

import (
	"empre_backend/internal/models"
	"time"

	"github.com/google/uuid"
)

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
	Category           CategoryResponse          `json:"category"`
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

	// Simplified Gallery
	Photos []PhotoResponse `json:"photos,omitempty"`
}

// EntityOwnerListDTO is for the owner's dashboard list.
type EntityOwnerListDTO struct {
	ID                 uuid.UUID                 `json:"id"`
	Name               string                    `json:"name"`
	CategoryName       string                    `json:"category_name"`
	ProfileURL         string                    `json:"profile_url"`
	VerificationStatus models.VerificationStatus `json:"verification_status"`
	IsVerified         bool                      `json:"is_verified"`
	CreatedAt          time.Time                 `json:"created_at"`
}
