package models

import (
	"testing"
	"time"
)

// La ventana del reclamo es la regla del producto (Backend v2 §4.2); si cambia,
// debe cambiar a propósito.
func TestOfferType_ClaimWindow(t *testing.T) {
	cases := map[OfferType]time.Duration{
		OfferHours: 30 * time.Minute,
		OfferDay:   3 * time.Hour,
		OfferWeek:  24 * time.Hour,
	}
	for typ, want := range cases {
		if got := typ.ClaimWindow(); got != want {
			t.Errorf("%s.ClaimWindow() = %v, se esperaba %v", typ, got, want)
		}
	}
	// Un tipo desconocido no debe dar una ventana de cero (el reclamo vencería al instante).
	if got := OfferType("OTRO").ClaimWindow(); got <= 0 {
		t.Errorf("un tipo desconocido dio una ventana de %v", got)
	}
}
