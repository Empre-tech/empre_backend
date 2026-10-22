package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestEffectiveStatus(t *testing.T) {
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		status  models.ClaimStatus
		expires time.Time
		want    string
	}{
		{"abierto y vigente", models.ClaimClaimed, now.Add(10 * time.Minute), "RECLAMADO"},
		{"abierto pero vencido: se muestra EXPIRADO sin esperar al barrido", models.ClaimClaimed, now.Add(-time.Second), "EXPIRADO"},
		{"vence justo ahora", models.ClaimClaimed, now, "EXPIRADO"},
		{"canjeado no cambia aunque su ventana haya pasado", models.ClaimRedeemed, now.Add(-time.Hour), "CANJEADO"},
		{"cancelado no cambia", models.ClaimCanceled, now.Add(-time.Hour), "CANCELADO"},
		{"expirado sigue expirado", models.ClaimExpired, now.Add(time.Hour), "EXPIRADO"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claim := &models.OfferClaim{Status: tc.status, ExpiresAt: tc.expires}
			if got := effectiveStatus(claim, now); got != tc.want {
				t.Errorf("effectiveStatus = %s, se esperaba %s", got, tc.want)
			}
		})
	}
}

func TestClaimResponse_CarriesEverythingTheQRScreenNeeds(t *testing.T) {
	redeemedAt := time.Now().Add(-5 * time.Minute)
	offer := &models.Offer{ID: uuid.New(), Type: models.OfferHours, Title: "Corte + barba", Price: "30000", TotalSlots: 10, UsedSlots: 7}
	claim := &models.OfferClaim{
		ID: uuid.New(), OfferID: offer.ID, Status: models.ClaimRedeemed, Token: "tok", ShortCode: "K7M2PQ",
		ClaimedAt: time.Now().Add(-20 * time.Minute), ExpiresAt: time.Now().Add(10 * time.Minute),
		RedeemedAt: &redeemedAt, VerifiedByClient: true,
	}

	out := claimResponse(&services.ClaimResult{Claim: claim, Offer: offer, EntityName: "Barbería El Corte Fino"})
	got := asJSON(t, out)

	for key, want := range map[string]any{
		"status":             "CANJEADO",
		"short_code":         "K7M2PQ",
		"entity_name":        "Barbería El Corte Fino",
		"verified_by_client": true,
	} {
		if got[key] != want {
			t.Errorf("%s = %v, se esperaba %v", key, got[key], want)
		}
	}
	if _, ok := got["redeemed_at"]; !ok {
		t.Error("falta redeemed_at")
	}

	// Sin canjear, redeemed_at no se envía.
	open := claimResponse(&services.ClaimResult{Claim: &models.OfferClaim{Status: models.ClaimClaimed, ExpiresAt: time.Now().Add(time.Hour)}, Offer: offer})
	if _, ok := asJSON(t, open)["redeemed_at"]; ok {
		t.Error("un reclamo sin canjear no debe traer redeemed_at")
	}
}

// La app muestra la regla antes de reclamar: la oferta publica su ventana.
func TestOfferResponse_PublishesClaimWindow(t *testing.T) {
	for typ, want := range map[models.OfferType]int{models.OfferHours: 30, models.OfferDay: 180, models.OfferWeek: 1440} {
		resp := dtos.OfferResponseFrom(&models.Offer{Type: typ, TotalSlots: 5})
		if resp.ClaimWindowMinutes != want {
			t.Errorf("%s: claim_window_minutes = %d, se esperaba %d", typ, resp.ClaimWindowMinutes, want)
		}
	}
}

// /claims/mine y /claims/:id conviven en el mismo grupo: la ruta fija no debe
// caer en la dinámica (ni el router entrar en pánico al registrarlas).
func TestRoutes_MineDoesNotFallIntoClaimByID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/claims")
	g.GET("/mine", func(c *gin.Context) { c.String(http.StatusOK, "mine") })
	g.GET("/:id", func(c *gin.Context) { c.String(http.StatusOK, "id:"+c.Param("id")) })

	for path, want := range map[string]string{
		"/api/claims/mine":    "mine",
		"/api/claims/abc-123": "id:abc-123",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK || w.Body.String() != want {
			t.Errorf("GET %s → %d %q, se esperaba 200 %q", path, w.Code, w.Body.String(), want)
		}
	}
}
