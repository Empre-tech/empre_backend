package models

import (
	"time"

	"github.com/google/uuid"
)

// OwnerNotice marca un aviso periódico ya enviado al dueño (p. ej. el de los fiados de la semana), para no repetirlo.
type OwnerNotice struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uniq_owner_notice,priority:1"`
	Kind     string    `gorm:"size:30;not null;uniqueIndex:uniq_owner_notice,priority:2"`
	Period   string    `gorm:"size:20;not null;uniqueIndex:uniq_owner_notice,priority:3"`
	SentAt   time.Time `gorm:"not null"`
}

func (OwnerNotice) TableName() string { return "owner_notices" }
