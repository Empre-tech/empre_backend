package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Category struct {
	ID   uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name string    `gorm:"not null" json:"name"`
	// Icon is an Ionicons name (e.g. "restaurant-outline") shown for the
	// category itself and for map markers of any business in it.
	Icon string `gorm:"not null;default:'storefront-outline'" json:"icon"`
	// Color is the brand hue for this category (portal frame/glow/glyph).
	// Always one of Empre's brand families (blue/orange/teal) — never an
	// arbitrary hue — so the map stays on-brand. Empty means the client falls
	// back to its own default.
	Color     string         `gorm:"type:varchar(9)" json:"color"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	Subcategories []Subcategory `gorm:"foreignKey:CategoryID" json:"-"`
}

func (Category) TableName() string {
	return "categories"
}
