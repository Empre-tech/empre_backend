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
}
