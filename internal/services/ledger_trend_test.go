package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
)

func saleAt(t time.Time, total int) models.Sale {
	return models.Sale{OccurredAt: t, Total: total}
}

func TestSalesByBucketIgnoraLoDeAfuera(t *testing.T) {
	loc := time.FixedZone("CO", -5*3600)
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 2)
	got := salesByBucket("day", from, to, []models.Sale{saleAt(from.Add(time.Hour), 10), saleAt(from.Add(26*time.Hour), 5), saleAt(to.Add(time.Hour), 99)})
	if len(got) != 2 || got[0] != 10 || got[1] != 5 {
		t.Fatalf("got %v", got)
	}
}

func TestNormalForDaysPromediaPorDiaDeLaSemana(t *testing.T) {
	loc := time.FixedZone("CO", -5*3600)
	monday := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	var history []models.Sale
	for w := 1; w <= 4; w++ {
		history = append(history, saleAt(monday.AddDate(0, 0, -7*w).Add(time.Hour), 100))
	}
	got := normalForDays(monday, monday.AddDate(0, 0, 2), history)
	if len(got) != 2 || got[0] != 100 || got[1] != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestNormalForDaysSinHistoriaNoDiceNada(t *testing.T) {
	loc := time.FixedZone("CO", -5*3600)
	monday := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	history := []models.Sale{saleAt(monday.AddDate(0, 0, -7), 100)}
	if got := normalForDays(monday, monday.AddDate(0, 0, 1), history); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestSourceTotalsReparteLasVentas(t *testing.T) {
	var s SourceTotals
	s.Add(models.SourceManual, 10)
	s.Add(models.SourceVoice, 5)
	s.Add(models.SourceOfferClaim, 20)
	s.Add(models.SourceAppointment, 30)
	if s.Manual != 15 || s.Offers != 20 || s.Agenda != 30 {
		t.Fatalf("%+v", s)
	}
}
