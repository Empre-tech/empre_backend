package services

import (
	"testing"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// TestVoiceOneSaleForManyProducts: «vendí unos zapatos y una gorra a David» es UNA venta con dos productos y el cliente
// una vez, aunque el modelo la devuelva partida. Dos clientes distintos siguen siendo dos ventas.
func TestVoiceOneSaleForManyProducts(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	db.Create(&models.ServiceItem{EntityID: ent.ID, Name: "Zapatos Lacoste", Price: ip(250000), Status: models.ServiceActive})
	db.Create(&models.ServiceItem{EntityID: ent.ID, Name: "Gorra", Price: ip(30000), Status: models.ServiceActive})
	david, _, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Name: "David Montes", Phone: "3001234567"})
	fm := &fakeModel{on: true}
	svc.SetVoiceModel(fm)

	fm.reply = `{"actions":[
		{"type":"sale","amount":0,"method":"cash","party_name":"David Montes","items":[{"name":"zapatos lacoste","qty":1}]},
		{"type":"sale","amount":0,"method":"","party_name":"","items":[{"name":"gorra","qty":2}]}]}`
	res, err := svc.ParseVoice(owner.ID, ent.ID, "vendí unos zapatos Lacoste y dos gorras a David Montes")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("una sola venta: %+v", res.Actions)
	}
	a := res.Actions[0]
	if len(a.Lines) != 2 || a.Amount != 310000 || a.PartyID == nil || *a.PartyID != david.ID {
		t.Fatalf("dos productos, $310.000, a David: %+v", a)
	}

	// El precio dicho de uno se queda en su renglón.
	fm.reply = `{"actions":[
		{"type":"sale","amount":200000,"amount_spoken":"doscientos mil","method":"cash","items":[{"name":"zapatos lacoste","qty":1}]},
		{"type":"sale","amount":0,"method":"cash","items":[{"name":"gorra","qty":1}]}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "vendí unos zapatos Lacoste en doscientos mil y una gorra")
	if len(res.Actions) != 1 || res.Actions[0].Amount != 230000 {
		t.Fatalf("una venta de $230.000: %+v", res.Actions)
	}

	fm.reply = `{"actions":[
		{"type":"sale","amount":0,"method":"cash","party_name":"David Montes","items":[{"name":"gorra","qty":1}]},
		{"type":"sale","amount":0,"method":"cash","party_name":"Laura","items":[{"name":"gorra","qty":1}]}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "vendí una gorra a David Montes y otra a Laura")
	if len(res.Actions) != 2 {
		t.Fatalf("dos clientes, dos ventas: %+v", res.Actions)
	}
}
