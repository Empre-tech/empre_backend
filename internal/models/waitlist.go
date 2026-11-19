package models

import (
	"time"

	"github.com/google/uuid"
)

// WaitlistStatus es en qué va cada persona de la lista de espera de un día.
type WaitlistStatus string

const (
	WaitWaiting WaitlistStatus = "waiting" // en la fila
	WaitOffered WaitlistStatus = "offered" // se le ofreció una hora que se liberó; tiene unos minutos para tomarla
	WaitBooked  WaitlistStatus = "booked"  // la tomó: ya tiene su cita
	WaitPassed  WaitlistStatus = "passed"  // no respondió a tiempo; la hora pasó al siguiente
	WaitLeft    WaitlistStatus = "left"    // se salió de la lista
)

// WaitlistEntry es una persona en la lista de espera de un negocio para un día (tablero «Agenda del negocio»,
// teléfono 3). Si alguien cancela, la hora se ofrece en orden de llegada, con unos minutos para cada uno.
type WaitlistEntry struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index:idx_waitlist_entity_day" json:"entity_id"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	// Day es el día en Colombia, «2026-11-14»: la lista es por día, no por hora.
	Day       string         `gorm:"type:varchar(10);not null;index:idx_waitlist_entity_day" json:"day"`
	ServiceID *uuid.UUID     `gorm:"type:uuid" json:"service_id,omitempty"`
	Status    WaitlistStatus `gorm:"type:varchar(12);not null;default:waiting;index" json:"status"`
	// La hora que se le ofreció y cuándo: de ahí sale cuánto le queda para tomarla.
	OfferedSlot   *time.Time `json:"offered_slot,omitempty"`
	OfferedAt     *time.Time `json:"offered_at,omitempty"`
	AppointmentID *uuid.UUID `gorm:"type:uuid" json:"appointment_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (WaitlistEntry) TableName() string {
	return "waitlist_entries"
}
