package models

import (
	"time"

	"github.com/google/uuid"
)

// OwnerProPlan: Empre Pro del DUEÑO, comprado en Google Play o en la App Store (2026-11-26, opción A). El nivel dice
// cuántos de sus negocios cubre (1, 2 o 3). Las tiendas no dejan tener dos veces la misma suscripción, así que el
// plan es uno por dueño y se sube o baja de nivel. Lo actualizan los avisos de RevenueCat.
type OwnerProPlan struct {
	UserID uuid.UUID `gorm:"type:uuid;primaryKey" json:"user_id"`
	// Tier: cuántos negocios cubre.
	Tier      int    `gorm:"not null" json:"tier"`
	ProductID string `gorm:"size:120" json:"product_id"`
	// Store: play_store | app_store | manual.
	Store string `gorm:"size:20" json:"store"`
	// WillRenew: falso si el dueño canceló; el plan sigue hasta CurrentPeriodEnd.
	WillRenew        bool      `gorm:"not null;default:true" json:"will_renew"`
	CurrentPeriodEnd time.Time `gorm:"not null" json:"current_period_end"`
	// LastEventAt: la hora del último aviso aplicado; uno más viejo que llegue tarde no lo pisa.
	LastEventAt time.Time `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (OwnerProPlan) TableName() string { return "owner_pro_plans" }

// ProCoverage: qué negocios del dueño cubre su plan. Se guarda aparte del plan: si el plan vence y se renueva, vuelven
// a quedar cubiertos los mismos.
type ProCoverage struct {
	EntityID  uuid.UUID `gorm:"type:uuid;primaryKey" json:"entity_id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (ProCoverage) TableName() string { return "pro_coverages" }

// StoreEvent: los avisos de RevenueCat ya aplicados (RevenueCat reintenta; el mismo aviso no se aplica dos veces).
type StoreEvent struct {
	ID        string    `gorm:"size:120;primaryKey" json:"id"`
	Type      string    `gorm:"size:40" json:"type"`
	UserID    string    `gorm:"size:120" json:"user_id"`
	ProductID string    `gorm:"size:120" json:"product_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (StoreEvent) TableName() string { return "store_events" }
