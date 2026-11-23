package models

import (
	"time"

	"github.com/google/uuid"
)

// EmailCode es el código de 6 dígitos para verificar el correo de una cuenta creada con contraseña. Uno por usuario:
// pedir otro reemplaza el anterior. Se guarda solo su hash.
type EmailCode struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	CodeHash  string    `gorm:"size:64;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	Attempts  int       `gorm:"not null;default:0"`
	CreatedAt time.Time
}

func (EmailCode) TableName() string { return "email_codes" }
