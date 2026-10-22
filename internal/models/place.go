package models

import (
	"time"

	"github.com/google/uuid"
)

// Place agrupa varios negocios que comparten el mismo punto físico (p. ej.
// un centro comercial o una esquina). Los negocios cercanos (< 50 m) se
// asignan al mismo place para poder mostrar un pin único con avatares
// apilados y el panel "En este lugar".
type Place struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Place) TableName() string {
	return "places"
}
