package models

import (
	"time"

	"github.com/google/uuid"
)

// EntityGoal es la meta del mes de un negocio (tablero «Actividad y meta», 2026-11-15): una sola por mes, la que elige
// el dueño. Kind «sales» = lo que vende; «left» = lo que le queda (entró menos salió).
type EntityGoal struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID  uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_goal_entity_month" json:"entity_id"`
	Month     string    `gorm:"size:7;not null;uniqueIndex:idx_goal_entity_month" json:"month"`
	Kind      string    `gorm:"size:10;not null" json:"kind"`
	Target    int64     `gorm:"not null" json:"target"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
