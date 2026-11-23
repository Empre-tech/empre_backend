package services

import (
	"testing"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// TestVoiceNamedClientIsCash: «vendí un bolso a David» es de contado a David (la IA a veces lo marcaba fiado solo por
// nombrar al cliente). Con una palabra de fiado sigue siendo fiado.
func TestVoiceNamedClientIsCash(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	db.Create(&models.ServiceItem{EntityID: ent.ID, Name: "Bolso", Price: ip(120000), Status: models.ServiceActive})
	david, _, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Name: "David Montes"})
	fm := &fakeModel{on: true}
	svc.SetVoiceModel(fm)

	fm.reply = `{"actions":[{"type":"sale","amount_spoken":"","amount":0,"method":"credit","party_name":"David Montes","items":[{"name":"bolso","qty":1}]}]}`
	res, err := svc.ParseVoice(owner.ID, ent.ID, "vendí un bolso a David Montes")
	if err != nil {
		t.Fatal(err)
	}
	a := res.Actions[0]
	if a.Method != models.PayCash || a.PartyID == nil || *a.PartyID != david.ID || len(a.Warnings) == 0 {
		t.Fatalf("de contado a David, con aviso: %+v", a)
	}

	fm.reply = `{"actions":[{"type":"sale","amount_spoken":"","amount":0,"method":"credit","party_name":"David Montes","items":[{"name":"bolso","qty":1}]}]}`
	res, err = svc.ParseVoice(owner.ID, ent.ID, "le fié un bolso a David Montes")
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions[0].Method != models.PayCredit || res.Actions[0].PartyID == nil {
		t.Fatalf("con «fié» sigue fiado: %+v", res.Actions[0])
	}

	fm.reply = `{"actions":[{"type":"sale","amount_spoken":"","amount":0,"method":"cash","party_name":"Laura","items":[{"name":"bolso","qty":1}]}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "vendí un bolso a Laura")
	if !res.Actions[0].NewParty || res.Actions[0].PartyName != "Laura" {
		t.Fatalf("una clienta nueva en una venta de contado queda para crearse: %+v", res.Actions[0])
	}

	// Al confirmar, el celular y la autorización que el dueño puso en la hoja quedan en la clienta nueva.
	acts := res.Actions
	acts[0].PartyPhone, acts[0].AcceptsMessages = "300 123 4567", true
	if _, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, acts); err != nil {
		t.Fatal(err)
	}
	var laura models.Party
	if err := db.Where("entity_id = ? AND name = ?", ent.ID, "Laura").First(&laura).Error; err != nil {
		t.Fatal(err)
	}
	if laura.Phone != "300 123 4567" || !laura.AcceptsMessages || laura.ConsentAt == nil {
		t.Fatalf("clienta nueva con su celular y autorización: %+v", laura)
	}
	var sale models.Sale
	if err := db.Where("party_id = ?", laura.ID).First(&sale).Error; err != nil || sale.Method != models.PayCash {
		t.Fatalf("la venta queda de contado a Laura: %+v %v", sale, err)
	}
}
