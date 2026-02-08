package dtos

import (
	"empre_backend/internal/models"

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
