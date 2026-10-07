package dtos

import (
	"time"

	"github.com/google/uuid"
)

// ClaimResponse is what the user sees when claiming / viewing their QR.
type ClaimResponse struct {
	ID        uuid.UUID     `json:"id"`
	OfferID   uuid.UUID     `json:"offer_id"`
	Status    string        `json:"status"`
	Token     string        `json:"token"`
	ShortCode string        `json:"short_code"`
	ClaimedAt time.Time     `json:"claimed_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	Offer     OfferResponse `json:"offer,omitempty"`
}

// RedeemResponse is the business scanner result ("Válido" or the rejection
// reason).
type RedeemResponse struct {
	Valid      bool      `json:"valid"`
	Reason     string    `json:"reason,omitempty"`
	ClaimID    uuid.UUID `json:"claim_id,omitempty"`
	EntityName string    `json:"entity_name,omitempty"`
	OfferTitle string    `json:"offer_title,omitempty"`
	ClientName string    `json:"client_name,omitempty"`
}
