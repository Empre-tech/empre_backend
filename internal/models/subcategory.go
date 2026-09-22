package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Subcategory belongs to a Category (e.g. Category "Comida" -> Subcategories
// "Cafetería", "Panadería", "Comida rápida", "Fit"). A business (Entity) can
// have several subcategories at once via the entity_subcategories join table.
type Subcategory struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name       string         `gorm:"not null" json:"name"`
	CategoryID uuid.UUID      `gorm:"type:uuid;not null;index" json:"category_id"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`

	Category Category `gorm:"foreignKey:CategoryID" json:"-"`
}

func (Subcategory) TableName() string {
	return "subcategories"
}
