package models

import (
	"time"

	"github.com/google/uuid"
)

// Zone es una zona con nombre de una ciudad («Centro Histórico», «El Rodadero»…): un círculo
// (centro y radio) curado por el equipo. Sirve para decirle a la persona en qué zona está y
// para agrupar negocios. Es dato, no código: una ciudad sin zonas simplemente no tiene filas.
type Zone struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	City      string    `gorm:"not null;index" json:"city"`
	Name      string    `gorm:"not null" json:"name"`
	Latitude  float64   `gorm:"not null" json:"latitude"`
	Longitude float64   `gorm:"not null" json:"longitude"`
	RadiusM   int       `gorm:"not null" json:"radius_m"`
	Active    bool      `gorm:"not null;default:true" json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Zone) TableName() string {
	return "zones"
}
