package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
)

func TestNextWaitingIsFirstToArrive(t *testing.T) {
	base := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	entries := []models.WaitlistEntry{
		{Status: models.WaitWaiting, CreatedAt: base.Add(2 * time.Minute)},
		{Status: models.WaitPassed, CreatedAt: base},
		{Status: models.WaitWaiting, CreatedAt: base.Add(1 * time.Minute)},
	}
	got := NextWaiting(entries)
	if got == nil || !got.CreatedAt.Equal(base.Add(1*time.Minute)) {
		t.Fatalf("debe ser el primero que sigue esperando, no el que ya pasó: %+v", got)
	}
	if NextWaiting([]models.WaitlistEntry{{Status: models.WaitBooked}}) != nil {
		t.Fatal("sin nadie esperando no hay siguiente")
	}
}

func TestOfferExpired(t *testing.T) {
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	recent := now.Add(-4 * time.Minute)
	old := now.Add(-OfferWindow)
	if OfferExpired(&recent, now) {
		t.Fatal("a los 4 minutos todavía puede tomarla")
	}
	if !OfferExpired(&old, now) || !OfferExpired(nil, now) {
		t.Fatal("a los 10 minutos, o sin oferta, ya no")
	}
}

func TestDayKeyIsColombia(t *testing.T) {
	// 2 am en UTC del 15 es todavía el 14 en Colombia (UTC−5).
	if got := DayKey(time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)); got != "2026-11-14" {
		t.Fatalf("día en Colombia: %s", got)
	}
}
