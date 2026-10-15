package dtos

import (
	"strings"
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

// RedeemResponse is the business scanner result: "Válido" with what to hand
// over, or a rejection with a machine-readable ReasonCode. Rejections are a
// normal outcome, so they travel as HTTP 200 with Valid=false; only malformed
// requests and system failures use error statuses.
//
// Fields marked "own business only" are never filled for WRONG_BUSINESS or
// NOT_FOUND, so a scan from another account reveals nothing.
type RedeemResponse struct {
	Valid      bool       `json:"valid"`
	Reason     string     `json:"reason,omitempty"`
	ReasonCode string     `json:"reason_code,omitempty"`
	ClaimID    *uuid.UUID `json:"claim_id,omitempty"`

	// Own business only.
	EntityName      string     `json:"entity_name,omitempty"`
	OfferTitle      string     `json:"offer_title,omitempty"`
	OfferPrice      string     `json:"offer_price,omitempty"`
	OfferConditions string     `json:"offer_conditions,omitempty"`
	ClientName      string     `json:"client_name,omitempty"` // first name + last initial
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	RedeemedAt      *time.Time `json:"redeemed_at,omitempty"`
	SlotsLeft       *int       `json:"slots_left,omitempty"`
	SlotsTotal      *int       `json:"slots_total,omitempty"`
}

// ShortName reduces a full name to "Nombre I." (first name and the initial of
// the last one), enough for the cashier to match the person without exposing
// the full name. A single word is returned as is.
func ShortName(full string) string {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	last := []rune(parts[len(parts)-1])
	return parts[0] + " " + strings.ToUpper(string(last[0])) + "."
}
