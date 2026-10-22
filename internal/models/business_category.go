package models

import "github.com/google/uuid"

// BusinessCategory vincula un negocio con sus categorías: una primaria
// (is_primary=true, máximo 1) y hasta dos secundarias. Sustituye gradualmente
// al vínculo único `entities.category_id` + `entity_subcategories`.
type BusinessCategory struct {
	BusinessID uuid.UUID `gorm:"type:uuid;primaryKey" json:"business_id"`
	CategoryID uuid.UUID `gorm:"type:uuid;primaryKey" json:"category_id"`
	IsPrimary  bool      `gorm:"default:false" json:"is_primary"`
}

func (BusinessCategory) TableName() string {
	return "business_categories"
}
