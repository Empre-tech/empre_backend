package models

import (
	"time"

	"github.com/google/uuid"
)

// SignalType is the kind of interaction recorded as a ranking signal.
type SignalType string

const (
	SignalView   SignalType = "view"   // profile viewed
	SignalSave   SignalType = "save"   // favorited
	SignalRedeem SignalType = "redeem" // offer redeemed
)

// Signal is a fire-and-forget telemetry event used to derive a business's
// popularity (a ranking variable). Not user-facing; appended, never updated.
type Signal struct {
	ID        uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID  uuid.UUID  `gorm:"type:uuid;not null;index" json:"entity_id"`
	Type      SignalType `gorm:"type:varchar(20);not null;index" json:"type"`
	CreatedAt time.Time  `gorm:"index" json:"created_at"`
}
