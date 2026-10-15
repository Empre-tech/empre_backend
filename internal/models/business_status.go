package models

import (
	"time"

	"github.com/google/uuid"
)

// BusinessStatus is the real-time availability a business communicates
// ("2 cupos disponibles", "3 mesas disponibles", "Producto disponible").
// It is independent of an offer's slots. If the business does not refresh it
// within the product TTL (6 hours), the app degrades to showing only the
// computed open/closed state.
// AvailabilityTTL es cuánto vale una disponibilidad sin que el negocio la
// actualice (decisión de producto: 6 horas).
const AvailabilityTTL = 6 * time.Hour

// Current devuelve la disponibilidad solo si sigue vigente: pasadas 6 horas
// sin actualizar se oculta y queda únicamente el abierto/cerrado por horario.
func (b BusinessStatus) Current(now time.Time) string {
	if b.Availability == "" || now.Sub(b.UpdatedAt) > AvailabilityTTL {
		return ""
	}
	return b.Availability
}

type BusinessStatus struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"entity_id"`
	Availability string    `json:"availability"`
	UpdatedAt    time.Time `json:"updated_at"`
}
