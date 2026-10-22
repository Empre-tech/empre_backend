package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/google/uuid"
)

func asJSON(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func sampleClaim(status models.ClaimStatus) (*models.OfferClaim, *models.Offer) {
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	redeemedAt := now.Add(14 * time.Minute)
	offer := &models.Offer{
		ID: uuid.New(), Title: "Corte + barba", Price: "30000", Conditions: "Solo con cita previa",
		TotalSlots: 10, UsedSlots: 8,
	}
	claim := &models.OfferClaim{
		ID: uuid.New(), OfferID: offer.ID, Status: status,
		ClaimedAt: now, ExpiresAt: now.Add(30 * time.Minute), RedeemedAt: &redeemedAt,
	}
	return claim, offer
}

func TestRedeemOutcome_RejectionsAreHTTP200WithReasonCode(t *testing.T) {
	claim, offer := sampleClaim(models.ClaimRedeemed)

	cases := []struct {
		name string
		rej  *services.RedeemRejection
		code string
	}{
		{"canjeado", &services.RedeemRejection{Code: services.ReasonAlreadyRedeemed, Message: "ya canjeado", Claim: claim, Offer: offer, ClientName: "María Fernanda Pérez"}, "ALREADY_REDEEMED"},
		{"vencido", &services.RedeemRejection{Code: services.ReasonExpired, Message: "reclamo expirado", Claim: claim, Offer: offer}, "EXPIRED"},
		{"cancelado", &services.RedeemRejection{Code: services.ReasonCanceled, Message: "reclamo cancelado", Claim: claim, Offer: offer}, "CANCELED"},
		{"otro negocio", &services.RedeemRejection{Code: services.ReasonWrongBusiness, Message: "este código no es de tu negocio"}, "WRONG_BUSINESS"},
		{"no existe", &services.RedeemRejection{Code: services.ReasonNotFound, Message: "código no válido"}, "NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// También cuando llega envuelto en otro error.
			status, body := redeemOutcome(nil, errors.Join(errors.New("ctx"), tc.rej))
			if status != http.StatusOK {
				t.Fatalf("status = %d, se esperaba 200", status)
			}
			got := asJSON(t, body)
			if got["valid"] != false {
				t.Errorf("valid = %v, se esperaba false", got["valid"])
			}
			if got["reason_code"] != tc.code {
				t.Errorf("reason_code = %v, se esperaba %s", got["reason_code"], tc.code)
			}
			if got["reason"] != tc.rej.Message {
				t.Errorf("reason = %v, se esperaba %q", got["reason"], tc.rej.Message)
			}
		})
	}
}

func TestRedeemOutcome_RejectionOfOwnClaimCarriesContext(t *testing.T) {
	claim, offer := sampleClaim(models.ClaimRedeemed)
	rej := &services.RedeemRejection{Code: services.ReasonAlreadyRedeemed, Message: "ya canjeado", Claim: claim, Offer: offer, ClientName: "María Fernanda Pérez"}

	_, body := redeemOutcome(nil, rej)
	got := asJSON(t, body)

	for key, want := range map[string]any{
		"offer_title":      "Corte + barba",
		"offer_price":      "30000",
		"offer_conditions": "Solo con cita previa",
		"client_name":      "María P.",
		"slots_left":       float64(2),
		"slots_total":      float64(10),
	} {
		if got[key] != want {
			t.Errorf("%s = %v, se esperaba %v", key, got[key], want)
		}
	}
	for _, key := range []string{"claimed_at", "expires_at", "redeemed_at", "claim_id"} {
		if _, ok := got[key]; !ok {
			t.Errorf("falta %s", key)
		}
	}
}

// Un escaneo desde otra cuenta no debe revelar nada del reclamo ni de la oferta.
func TestRedeemOutcome_WrongBusinessAndNotFoundLeakNothing(t *testing.T) {
	for _, code := range []services.RedeemReason{services.ReasonWrongBusiness, services.ReasonNotFound} {
		_, body := redeemOutcome(nil, &services.RedeemRejection{Code: code, Message: "x"})
		got := asJSON(t, body)
		for _, key := range []string{"claim_id", "entity_name", "offer_title", "offer_price", "offer_conditions", "client_name", "claimed_at", "expires_at", "redeemed_at", "slots_left", "slots_total"} {
			if v, ok := got[key]; ok {
				t.Errorf("%s: la respuesta incluye %s = %v", code, key, v)
			}
		}
	}
}

func TestRedeemOutcome_ZeroSlotsLeftIsReported(t *testing.T) {
	claim, offer := sampleClaim(models.ClaimClaimed)
	offer.UsedSlots = offer.TotalSlots
	got := asJSON(t, redeemValidResponse(&services.ClaimResult{Claim: claim, Offer: offer, ClientName: "Ana Gómez", EntityName: "Barbería"}))
	if v, ok := got["slots_left"]; !ok || v != float64(0) {
		t.Errorf("slots_left = %v (presente: %v), se esperaba 0 presente", v, ok)
	}
}

func TestRedeemOutcome_ValidCarriesWhatToHandOver(t *testing.T) {
	claim, offer := sampleClaim(models.ClaimRedeemed)
	status, body := redeemOutcome(&services.ClaimResult{Claim: claim, Offer: offer, ClientName: "María Fernanda Pérez", EntityName: "Barbería El Corte Fino"}, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	got := asJSON(t, body)
	for key, want := range map[string]any{
		"valid": true, "entity_name": "Barbería El Corte Fino", "offer_title": "Corte + barba",
		"offer_price": "30000", "offer_conditions": "Solo con cita previa", "client_name": "María P.",
		"slots_left": float64(2),
	} {
		if got[key] != want {
			t.Errorf("%s = %v, se esperaba %v", key, got[key], want)
		}
	}
	if _, ok := got["reason_code"]; ok {
		t.Error("una respuesta válida no debe traer reason_code")
	}
}

// Un fallo real del sistema no se disfraza de rechazo.
func TestRedeemOutcome_SystemFailureIs500(t *testing.T) {
	status, body := redeemOutcome(nil, errors.New("conexión con la base de datos perdida"))
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, se esperaba 500", status)
	}
	got := asJSON(t, body)
	if got["error"] == nil || got["valid"] != nil {
		t.Errorf("cuerpo = %v, se esperaba solo {error}", got)
	}
}

func TestShortName(t *testing.T) {
	cases := map[string]string{
		"María Fernanda Pérez": "María P.",
		"Ana Gómez":            "Ana G.",
		"  luis   ramírez  ":   "luis R.",
		"Cher":                 "Cher",
		"":                     "",
		"Óscar Ñandú":          "Óscar Ñ.",
	}
	for in, want := range cases {
		if got := dtos.ShortName(in); got != want {
			t.Errorf("ShortName(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}
