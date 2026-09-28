package models

import "github.com/google/uuid"

// BusinessHour is one weekday's schedule for an entity. Weekday follows Go's
// time.Weekday numbering (0=domingo ... 6=sábado) so it lines up directly
// with time.Now().Weekday() when checking whether a business is open now.
type BusinessHour struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_entity_weekday" json:"entity_id"`
	Weekday  int       `gorm:"not null;uniqueIndex:idx_entity_weekday" json:"weekday"`
	// Closed takes priority over Is24h; Is24h takes priority over Open/CloseTime.
	Closed bool `gorm:"default:false" json:"closed"`
	Is24h  bool `gorm:"default:false" json:"is_24h"`
	// "HH:MM" (24h). Vacío si Closed o Is24h. CloseTime puede ser menor que
	// OpenTime (p. ej. abre 18:00, cierra 02:00): significa que cruza la
	// medianoche, muy común en bares y vida nocturna.
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
}

func (BusinessHour) TableName() string {
	return "business_hours"
}

// ServiceMode is how a business delivers what it offers.
type ServiceMode string

const (
	ServiceModeInPlace  ServiceMode = "in_place"
	ServiceModeDelivery ServiceMode = "delivery"
	ServiceModeBoth     ServiceMode = "both"
)

func (m ServiceMode) Valid() bool {
	switch m {
	case ServiceModeInPlace, ServiceModeDelivery, ServiceModeBoth, "":
		return true
	default:
		return false
	}
}
