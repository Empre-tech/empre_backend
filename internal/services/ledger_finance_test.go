package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
)

func TestDailyTotalsSplitsByColombianDay(t *testing.T) {
	loc := time.FixedZone("COT", -5*3600)
	from := time.Date(2026, 10, 19, 0, 0, 0, 0, loc) // lunes
	to := from.AddDate(0, 0, 7)
	sales := []models.Sale{
		{Total: 20000, Status: models.LedgerActive, OccurredAt: time.Date(2026, 10, 19, 9, 0, 0, 0, loc)},
		// 11 pm en Colombia es el día siguiente en UTC: cuenta para el lunes.
		{Total: 5000, Status: models.LedgerActive, OccurredAt: time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC)},
		{Total: 9000, Status: models.LedgerVoided, OccurredAt: time.Date(2026, 10, 19, 10, 0, 0, 0, loc)},
		{Total: 7000, Status: models.LedgerActive, OccurredAt: time.Date(2026, 10, 26, 1, 0, 0, 0, loc)}, // fuera
	}
	exps := []models.Expense{{Amount: 3000, Status: models.LedgerActive, OccurredAt: time.Date(2026, 10, 25, 12, 0, 0, 0, loc)}}
	days := DailyTotals(sales, exps, from, to)
	if len(days) != 7 {
		t.Fatalf("want 7 days, got %d", len(days))
	}
	if days[0].Date != "2026-10-19" || days[0].Sales != 25000 {
		t.Fatalf("monday: %+v", days[0])
	}
	if days[6].Expenses != 3000 || days[6].Sales != 0 {
		t.Fatalf("sunday: %+v", days[6])
	}
}

func TestUsualExpensesKeepsRepeatedOnes(t *testing.T) {
	at := func(d int) time.Time { return time.Date(2026, 10, d, 12, 0, 0, 0, time.UTC) }
	exps := []models.Expense{
		{Note: "Taxi", Category: "transporte", Amount: 7000, Status: models.LedgerActive, OccurredAt: at(1)},
		{Note: "taxi ", Category: "transporte", Amount: 8000, Status: models.LedgerActive, OccurredAt: at(5)},
		{Note: "Taxi", Category: "transporte", Amount: 6000, Status: models.LedgerActive, OccurredAt: at(3)},
		{Category: "servicios", Amount: 50000, Status: models.LedgerActive, OccurredAt: at(2)},
		{Category: "servicios", Amount: 52000, Status: models.LedgerActive, OccurredAt: at(9)},
		{Note: "Arriendo", Category: "arriendo", Amount: 900000, Status: models.LedgerActive, OccurredAt: at(1)}, // una vez
		{Note: "Taxi", Category: "transporte", Amount: 1, Status: models.LedgerVoided, OccurredAt: at(10)},
	}
	got := UsualExpenses(exps, 4)
	if len(got) != 2 {
		t.Fatalf("want 2, got %+v", got)
	}
	if got[0].Count != 3 || got[0].Amount != 8000 || got[0].Note != "taxi" {
		t.Fatalf("taxi first with the latest amount: %+v", got[0])
	}
	if got[1].Key != "cat:servicios" || got[1].Amount != 52000 || got[1].Note != "" {
		t.Fatalf("services by category: %+v", got[1])
	}
	if one := UsualExpenses(exps, 1); len(one) != 1 {
		t.Fatalf("limit: %+v", one)
	}
}
