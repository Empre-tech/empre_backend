package models

import (
	"time"

	"github.com/google/uuid"
)

// AskQuestion es una pregunta distinta que un usuario le hizo a «Pregúntale a Empre» en un mes (2026-11-22). Cuenta
// para su cupo mensual; repetir la misma pregunta (cambiar un filtro, volver a buscar) no cuenta otra vez.
type AskQuestion struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_ask_questions_user_month_key,priority:1" json:"user_id"`
	Month     string    `gorm:"size:7;not null;uniqueIndex:idx_ask_questions_user_month_key,priority:2" json:"month"` // "2026-11", hora de Colombia
	Key       string    `gorm:"size:300;not null;uniqueIndex:idx_ask_questions_user_month_key,priority:3" json:"-"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

func (AskQuestion) TableName() string { return "ask_questions" }

// UserPlan es la suscripción de un usuario (no de un negocio): hoy «plus», 300 preguntas al mes a «Pregúntale a
// Empre». Activa mientras CurrentPeriodEnd esté en el futuro. La activan las compras de las tiendas (o un admin).
type UserPlan struct {
	UserID           uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	Plan             string    `gorm:"size:20;not null;default:'plus'" json:"plan"`
	CurrentPeriodEnd time.Time `gorm:"not null" json:"current_period_end"`
	Source           string    `gorm:"size:20;not null;default:'manual'" json:"source"` // manual | play | app_store
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (UserPlan) TableName() string { return "user_plans" }
