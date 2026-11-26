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
	// HasPassword: false si la cuenta entra solo con Google (al eliminarla no se pide contraseña).
	HasPassword bool `json:"has_password"`
	// EmailVerified: el correo está verificado (código, Google o restablecer la contraseña).
	EmailVerified bool `json:"email_verified"`
	// TermsVersion: la versión de la política y los términos que aceptó; la app la compara con LegalVersion.
	TermsVersion string `json:"terms_version"`
	LegalVersion string `json:"legal_version"`
}
