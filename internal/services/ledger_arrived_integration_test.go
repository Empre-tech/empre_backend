package services

import (
	"testing"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// «Ya llegó»: suma lo que llegó, registra la compra si se dice cuánto se pagó y saca el producto de agotado.
func TestArrived(t *testing.T) {
	db, svc, owner, other, ent := ledgerFixture(t)
	huevos := stockProduct(t, db, ent, "Huevos", 25_000)
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: huevos.ID, Counted: fp(0), Reason: "conteo"}); err != nil {
		t.Fatal(err)
	}
	soldOut := func(id uuid.UUID) bool {
		var it models.ServiceItem
		if err := db.First(&it, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return it.SoldOut
	}
	if !soldOut(huevos.ID) {
		t.Fatal("con 0 debe verse agotado")
	}

	if _, err := svc.Arrived(other.ID, ent.ID, huevos.ID, ArrivedInput{ClientID: uuid.New(), Qty: 5}); err == nil {
		t.Fatal("otro usuario no puede")
	}
	if _, err := svc.Arrived(owner.ID, ent.ID, huevos.ID, ArrivedInput{ClientID: uuid.New()}); !isInvalid(err) {
		t.Fatal("en cantidad exacta hay que decir cuántos llegaron")
	}
	if _, err := svc.Arrived(owner.ID, ent.ID, huevos.ID, ArrivedInput{ClientID: uuid.New(), Qty: 5, Paid: 1000, Method: "credit"}); !isInvalid(err) {
		t.Fatal("a crédito va por Gastar")
	}

	// Sin pago: solo suma.
	ov, err := svc.Arrived(owner.ID, ent.ID, huevos.ID, ArrivedInput{ClientID: uuid.New(), Qty: 4})
	if err != nil {
		t.Fatal(err)
	}
	if it := findInv(t, ov, "Huevos"); it.Qty != 4 || soldOut(huevos.ID) {
		t.Fatalf("sin pago: %+v", it)
	}

	// Con pago: suma, registra la compra, actualiza el costo y recuerda la cantidad. Repetir no duplica.
	cid := uuid.New()
	for i := 0; i < 2; i++ {
		if ov, err = svc.Arrived(owner.ID, ent.ID, huevos.ID, ArrivedInput{ClientID: cid, Qty: 20, Paid: 380_000, Method: "transfer"}); err != nil {
			t.Fatal(err)
		}
	}
	it := findInv(t, ov, "Huevos")
	if it.Qty != 24 || it.Cost == nil || *it.Cost != 19_000 || it.LastPurchase == nil || *it.LastPurchase != 20 {
		t.Fatalf("con pago: qty %v cost %v last %v", it.Qty, it.Cost, it.LastPurchase)
	}
	var exps []models.Expense
	db.Where("entity_id = ?", ent.ID).Find(&exps)
	if len(exps) != 1 || exps[0].Amount != 380_000 || exps[0].Category != "mercancia" || exps[0].Method != "transfer" {
		t.Fatalf("gasto: %+v", exps)
	}

	// Un insumo en semáforo que se acabó: vuelve a «hay».
	sal, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Sal"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetLevel(owner.ID, ent.ID, sal.ID, "out", uuid.New()); err != nil {
		t.Fatal(err)
	}
	if ov, err = svc.Arrived(owner.ID, ent.ID, sal.ID, ArrivedInput{ClientID: uuid.New()}); err != nil {
		t.Fatal(err)
	}
	if s := findInv(t, ov, "Sal"); s.Status != "ok" {
		t.Fatalf("semáforo: %+v", s)
	}

	// Exacto que nunca se contó, con pago: lo que llegó es lo que hay.
	cafe := stockProduct(t, db, ent, "Café", 12_000)
	if _, err := svc.UpdateStockSettings(owner.ID, ent.ID, cafe.ID, ItemSettings{Mode: strp("exact")}); err != nil {
		t.Fatal(err)
	}
	if ov, err = svc.Arrived(owner.ID, ent.ID, cafe.ID, ArrivedInput{ClientID: uuid.New(), Qty: 10, Paid: 80_000}); err != nil {
		t.Fatal(err)
	}
	if c := findInv(t, ov, "Café"); c.Qty != 10 || !c.Counted || c.Cost == nil || *c.Cost != 8_000 {
		t.Fatalf("sin contar: %+v", c)
	}
}
