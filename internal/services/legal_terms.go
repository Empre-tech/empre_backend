package services

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// LegalVersion es la versión vigente de la Política de tratamiento de datos y los Términos (2026-11-25). Cuando cambie
// el texto de cualquiera de las dos, súbela: la app vuelve a pedir la aceptación a todos.
const LegalVersion = "2026-11-25"

// AcceptTerms guarda que el usuario aceptó la versión vigente y cuándo (prueba de la autorización, Ley 1581 art. 9).
func (s *UserService) AcceptTerms(userID uuid.UUID, now time.Time) error {
	return s.Repo.DB.Model(&models.User{}).Where("id = ?", userID).
		Updates(map[string]any{"terms_version": LegalVersion, "terms_accepted_at": now}).Error
}
