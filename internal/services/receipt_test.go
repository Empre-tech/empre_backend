package services

import (
	"errors"
	"strings"
	"testing"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// TestReceiptAndCustomer: un cliente con su autorización, una venta a su nombre, el ticket público (solo el primer
// nombre, renglones, total y medio de pago) y el historial del cliente.
func TestReceiptAndCustomer(t *testing.T) {
	_, svc, owner, other, ent := ledgerFixture(t)
	cli, _, err := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Name: "Ana María Pérez", Phone: "300 123 4567", AcceptsMessages: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cli.AcceptsMessages || cli.ConsentAt == nil {
		t.Fatalf("la autorización debía quedar guardada: %+v", cli)
	}
	price := 12000
	sale, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: models.PayCash, PartyID: &cli.ID,
		Lines: []LineInput{{Name: "Pan de queso", Qty: 2, UnitPrice: &price}}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.PublicReceipt(sale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Customer != "Ana" || r.Total != "$24.000" || len(r.Lines) != 1 || r.Lines[0].Price != "$24.000" || !strings.HasPrefix(r.Pays[0], "Efectivo") || r.Business != ent.Name {
		t.Fatalf("ticket: %+v", r)
	}
	if _, err := svc.PublicReceipt(uuid.New()); !errors.Is(err, ErrLedgerNotFound) {
		t.Fatalf("venta que no existe: %v", err)
	}

	mine, err := svc.ListSales(owner.ID, ent.ID, ListFilter{PartyID: &cli.ID})
	if err != nil || len(mine) != 1 {
		t.Fatalf("historial del cliente: %d %v", len(mine), err)
	}

	off := false
	upd, err := svc.UpdateParty(owner.ID, ent.ID, cli.ID, PartyUpdate{AcceptsMessages: &off})
	if err != nil || upd.AcceptsMessages || upd.ConsentAt != nil {
		t.Fatalf("quitar la autorización: %+v %v", upd, err)
	}
	if _, err := svc.UpdateParty(other.ID, ent.ID, cli.ID, PartyUpdate{AcceptsMessages: &off}); err == nil {
		t.Fatal("otro usuario no puede cambiar los clientes del negocio")
	}
}
