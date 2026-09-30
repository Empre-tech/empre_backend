package models

import (
	"time"

	"github.com/google/uuid"
)

// SubscriptionStatus mirrors where a business's paid plan stands.
type SubscriptionStatus string

const (
	SubscriptionInactive SubscriptionStatus = "inactive" // nunca ha pagado, o venció y no ha renovado
	SubscriptionActive   SubscriptionStatus = "active"
)

// Subscription is the paid-plan state for one business (Entity). There's at
// most one row per entity: paying again while active just extends
// CurrentPeriodEnd instead of creating a second row.
type Subscription struct {
	ID     uuid.UUID          `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID        `gorm:"type:uuid;not null;uniqueIndex" json:"entity_id"`
	Plan   string             `gorm:"not null;default:'pro_monthly'" json:"plan"`
	Status SubscriptionStatus `gorm:"type:varchar(20);not null;default:'inactive'" json:"status"`
	// Hasta cuándo queda cubierto el plan. nil si nunca se ha pagado.
	CurrentPeriodEnd *time.Time `json:"current_period_end,omitempty"`
	// Cuándo se le avisó al dueño (push) que ya superó el umbral de clientes
	// distintos gratis y debería activar el plan. nil = todavía no se le ha
	// avisado (evita mandarle el mismo aviso con cada mensaje nuevo).
	TrialThresholdNotifiedAt *time.Time `json:"-"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

func (Subscription) TableName() string {
	return "subscriptions"
}

// PaymentStatus mirrors Wompi's transaction status values we care about.
type PaymentStatus string

const (
	PaymentPending  PaymentStatus = "PENDING"
	PaymentApproved PaymentStatus = "APPROVED"
	PaymentDeclined PaymentStatus = "DECLINED"
	PaymentError    PaymentStatus = "ERROR"
	PaymentVoided   PaymentStatus = "VOIDED"
)

// SubscriptionPayment is one checkout attempt (a Wompi "reference"). It is
// created PENDING right when we hand the reference to the app, and updated
// by the Wompi webhook once the customer finishes paying. Keeping every
// attempt (not just the latest) gives an audit trail for money that moved.
type SubscriptionPayment struct {
	ID             uuid.UUID     `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	SubscriptionID uuid.UUID     `gorm:"type:uuid;not null;index" json:"subscription_id"`
	EntityID       uuid.UUID     `gorm:"type:uuid;not null;index" json:"entity_id"`
	// Reference es el identificador que nosotros generamos y que viaja al
	// widget de Wompi; es lo único que tenemos para casar el webhook con este
	// intento de pago (Wompi no nos devuelve nuestro subscription_id).
	Reference       string        `gorm:"not null;uniqueIndex" json:"reference"`
	AmountInCents   int64         `gorm:"not null" json:"amount_in_cents"`
	Currency        string        `gorm:"not null;default:'COP'" json:"currency"`
	Status          PaymentStatus `gorm:"type:varchar(20);not null;default:'PENDING'" json:"status"`
	WompiTransactionID string     `json:"wompi_transaction_id,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

func (SubscriptionPayment) TableName() string {
	return "subscription_payments"
}
