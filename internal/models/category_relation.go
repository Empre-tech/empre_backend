package models

import "github.com/google/uuid"

// CategoryRelation conecta dos categorías con un peso (relevancia de la
// relación), usado en el nivel "related" de la búsqueda: solo se devuelven
// categorías cuyo peso supere un umbral.
type CategoryRelation struct {
	FromID uuid.UUID `gorm:"type:uuid;primaryKey" json:"from_id"`
	ToID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"to_id"`
	Weight float64   `gorm:"not null" json:"weight"`
}

func (CategoryRelation) TableName() string {
	return "category_relations"
}
