package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ClaimStatus is the lifecycle of a single offer claim.
type ClaimStatus string

const (
	ClaimClaimed  ClaimStatus = "RECLAMADO" // user has the QR, a slot is reserved
	ClaimRedeemed ClaimStatus = "CANJEADO"  // business scanned it and it was valid
	ClaimExpired  ClaimStatus = "EXPIRADO"  // not used in time; the slot is released
	ClaimCanceled ClaimStatus = "CANCELADO" // user released the slot
)

// OpenClaimIndex is the partial unique index that allows at most one
// RECLAMADO claim per user and offer (created by database.EnsureClaimIndexes).
const OpenClaimIndex = "uniq_offer_claims_open_per_user"

// OfferClaim ties one user to one offer publication. The QR is of the CLAIM
// (not the offer), so it is bound to a person and consumed exactly once; a
// leaked screenshot is only valid for the first scan.
//
// Redemption is embedded here rather than a separate table: a claim can be
// redeemed at most once, so the audit fields (who scanned, when, from which
// device, whether the client confirmed) fit naturally on the claim row.
type OfferClaim struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OfferID  uuid.UUID `gorm:"type:uuid;not null;index" json:"offer_id"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`

	Status ClaimStatus `gorm:"type:varchar(20);default:'RECLAMADO';index" json:"status"`

	// Token is the opaque QR payload; ShortCode is the 6-char manual fallback.
	Token     string `gorm:"uniqueIndex;not null" json:"token"`
	ShortCode string `gorm:"index;not null" json:"short_code"`

	ClaimedAt time.Time `json:"claimed_at"`
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`

	// Redemption / audit.
	RedeemedAt       *time.Time `json:"redeemed_at,omitempty"`
	RedeemedBy       *uuid.UUID `gorm:"type:uuid" json:"redeemed_by,omitempty"`
	RedeemedDevice   string     `json:"redeemed_device,omitempty"`
	VerifiedByClient bool       `gorm:"default:false" json:"verified_by_client"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
