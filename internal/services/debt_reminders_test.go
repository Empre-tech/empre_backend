package services

import (
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// TestDebtReminderWeekly: el lunes a las 10 a. m. sale un solo aviso al dueño con lo que le deben; otro día no sale y
// el mismo lunes no se repite.
func TestDebtReminderWeekly(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	if err := db.AutoMigrate(&models.OwnerNotice{}); err != nil {
		t.Fatal(err)
	}
	type sent struct {
		to          uuid.UUID
		title, body string
		data        map[string]string
	}
	var got []sent
	svc.Notify = func(to uuid.UUID, title, body string, data map[string]string) {
		got = append(got, sent{to, title, body, data})
	}

	ana, _, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Name: "Ana"})
	total := 30000
	at := time.Now().AddDate(0, 0, -10)
	if _, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Total: &total, Method: models.PayCredit, PartyID: &ana.ID, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}

	loc, _ := time.LoadLocation("America/Bogota")
	monday := time.Date(2026, 11, 23, 10, 0, 0, 0, loc) // lunes
	if n := svc.DebtReminderTick(monday.AddDate(0, 0, 1)); n != 0 {
		t.Fatalf("un martes no sale: %d", n)
	}
	if n := svc.DebtReminderTick(monday); n != 1 || len(got) != 1 {
		t.Fatalf("el lunes sale uno: %d %+v", n, got)
	}
	g := got[0]
	if g.to != owner.ID || g.title != "Te deben $30.000" || !strings.HasPrefix(g.body, "Ana te debe $30.000") || g.data["type"] != "debts" || g.data["entity_id"] != ent.ID.String() {
		t.Fatalf("aviso: %+v", g)
	}
	if n := svc.DebtReminderTick(monday.Add(2 * time.Hour)); n != 0 {
		t.Fatalf("el mismo lunes no se repite: %d", n)
	}
}
