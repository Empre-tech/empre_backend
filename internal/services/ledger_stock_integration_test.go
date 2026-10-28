package services

import (
	"strings"
	"testing"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func stockProduct(t *testing.T, db *gorm.DB, ent models.Entity, name string, price int) models.ServiceItem {
	it := models.ServiceItem{EntityID: ent.ID, Name: name, Price: ip(price), Status: models.ServiceActive}
	if err := db.Create(&it).Error; err != nil {
		t.Fatal(err)
	}
	return it
}

func findInv(t *testing.T, ov InventoryOverview, name string) InventoryItem {
	for _, i := range ov.Items {
		if i.Name == name {
			return i
		}
	}
	t.Fatalf("no está %q en el inventario", name)
	return InventoryItem{}
}

func TestInventoryFlow(t *testing.T) {
	db, svc, owner, other, ent := ledgerFixture(t)
	jugo := stockProduct(t, db, ent, "Jugo Hit", 3000)
	reloj := stockProduct(t, db, ent, "Reloj", 120000)

	if _, err := svc.Inventory(other.ID, ent.ID); err != ErrLedgerForbidden {
		t.Fatalf("otro usuario: %v", err)
	}

	// Sin decidir: todos «sin configurar», nada se descuenta.
	ov, err := svc.Inventory(owner.ID, ent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Unset != 2 {
		t.Fatalf("esperaba 2 sin configurar, got %d", ov.Unset)
	}

	// Un toque: lo recomendado. Categoría «Comida» → platos sin stock; el reloj (caro) → exacta.
	ov, err = svc.SetupInventory(owner.ID, ent.ID, SetupRecommended)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Unset != 0 {
		t.Fatalf("tras configurar no debe quedar nada sin decidir: %d", ov.Unset)
	}
	if got := findInv(t, ov, "Reloj").Mode; got != "exact" {
		t.Fatalf("producto caro debe ser exacto, got %s", got)
	}

	// Contar el reloj: hay 5. Vender 2 descuenta; anular devuelve.
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: reloj.ID, Counted: fp(5), Reason: "conteo"}); err != nil {
		t.Fatal(err)
	}
	sid := uuid.New()
	if _, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: sid, Method: "cash", Lines: []LineInput{{ItemID: &reloj.ID, Qty: 2}}}); err != nil {
		t.Fatal(err)
	}
	// Reenvío: no descuenta dos veces.
	_, _, _ = svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: sid, Method: "cash", Lines: []LineInput{{ItemID: &reloj.ID, Qty: 2}}})
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Reloj").Qty; q != 3 {
		t.Fatalf("esperaba 3 relojes, got %v", q)
	}
	if _, err := svc.VoidSale(owner.ID, ent.ID, sid, "error"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VoidSale(owner.ID, ent.ID, sid, "error"); err != nil {
		t.Fatal(err)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Reloj").Qty; q != 5 {
		t.Fatalf("anular devuelve: esperaba 5, got %v", q)
	}

	// Vender más de lo que hay no bloquea la venta.
	if _, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "cash", Lines: []LineInput{{ItemID: &reloj.ID, Qty: 9}}}); err != nil {
		t.Fatalf("vender sin existencias no debe fallar: %v", err)
	}

	// Compra: sube existencias y actualiza el costo.
	eid := uuid.New()
	e, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: eid, Category: "mercancia", Method: "cash",
		Lines: []PurchaseLine{{ItemID: &reloj.ID, Qty: 10, UnitCost: 70000}}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Amount != 700000 {
		t.Fatalf("el total sale de los renglones: %d", e.Amount)
	}
	var p models.ServiceItem
	db.First(&p, "id = ?", reloj.ID)
	if p.Cost == nil || *p.Cost != 70000 {
		t.Fatalf("costo no actualizado: %v", p.Cost)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Reloj").Qty; q != 6 { // 5 - 9 + 10
		t.Fatalf("esperaba 6, got %v", q)
	}
	if _, err := svc.VoidExpense(owner.ID, ent.ID, eid, "mal"); err != nil {
		t.Fatal(err)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Reloj").Qty; q != -4 {
		t.Fatalf("anular compra: esperaba -4, got %v", q)
	}

	// Ajuste con motivo (pérdida) y motivo inválido.
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: reloj.ID, Counted: fp(4), Reason: "conteo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: reloj.ID, Delta: fp(-1), Reason: "danado"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: reloj.ID, Delta: fp(-1), Reason: "ni idea"}); !isInvalid(err) {
		t.Fatalf("motivo inválido debe fallar: %v", err)
	}

	// Semáforo: «se acabó» marca agotado en el catálogo público; «hay» lo quita.
	if err := svc.SetLevel(owner.ID, ent.ID, jugo.ID, "out", uuid.New()); err != nil {
		t.Fatal(err)
	}
	var j models.ServiceItem
	db.First(&j, "id = ?", jugo.ID)
	if !j.SoldOut {
		t.Fatal("agotado debe verse en el catálogo")
	}
	if err := svc.SetLevel(owner.ID, ent.ID, jugo.ID, "ok", uuid.New()); err != nil {
		t.Fatal(err)
	}
	db.First(&j, "id = ?", jugo.ID)
	if j.SoldOut {
		t.Fatal("al volver a haber, ya no está agotado")
	}
	if err := svc.SetLevel(owner.ID, ent.ID, jugo.ID, "mucho", uuid.New()); !isInvalid(err) {
		t.Fatalf("nivel inválido: %v", err)
	}

	// Insumos.
	sup, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Lechuga"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: " jugo  HIT "}); !isInvalid(err) {
		t.Fatalf("un insumo no puede repetir un producto: %v", err)
	}
	again, _ := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: " lechuga "})
	if again.ID != sup.ID {
		t.Fatal("el mismo insumo no se duplica")
	}
	if err := svc.SetLevel(owner.ID, ent.ID, sup.ID, "low", uuid.New()); err != nil {
		t.Fatal(err)
	}
	ins, err := svc.Insights(owner.ID, ent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) == 0 {
		t.Fatal("debería avisar de la lechuga que queda poca")
	}
	if err := svc.ArchiveSupply(owner.ID, ent.ID, sup.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveSupply(other.ID, ent.ID, sup.ID); err != ErrLedgerForbidden {
		t.Fatalf("otro usuario: %v", err)
	}
}

func TestInventorySetupChoices(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	stockProduct(t, db, ent, "Arepa", 2000)
	ov, err := svc.SetupInventory(owner.ID, ent.ID, SetupSimple)
	if err != nil {
		t.Fatal(err)
	}
	if findInv(t, ov, "Arepa").Mode != "light" {
		t.Fatal("lo más simple es semáforo")
	}
	if _, err := svc.SetupInventory(owner.ID, ent.ID, "otra"); !isInvalid(err) {
		t.Fatalf("opción inválida: %v", err)
	}
}

func fp(f float64) *float64 { return &f }

func TestVoiceStock(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	jugo := stockProduct(t, db, ent, "Jugo Hit", 3000)
	camisa := stockProduct(t, db, ent, "Camisa", 60000)
	if _, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Lechuga"}); err != nil {
		t.Fatal(err)
	}
	fm := &fakeModel{on: true}
	svc.SetVoiceModel(fm)
	fm.reply = `{"actions":[
		{"type":"stock","stock_name":"jugo hit","stock_level":"out"},
		{"type":"stock","stock_name":"camisas","stock_counted":12},
		{"type":"stock","stock_name":"lechuga","stock_level":"low"},
		{"type":"stock","stock_name":"pizza","stock_level":"out"}
	]}`
	res, err := svc.ParseVoice(owner.ID, ent.ID, "se acabó el jugo hit, tengo 12 camisas, queda poca lechuga y se acabó la pizza")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 4 || res.Actions[0].ItemRef == nil || res.Actions[1].Counted == nil || res.Actions[2].ItemRef == nil {
		t.Fatalf("acciones: %+v", res.Actions)
	}
	if res.Actions[3].ItemRef != nil || len(res.Actions[3].Warnings) == 0 {
		t.Fatalf("la pizza no existe: %+v", res.Actions[3])
	}
	// Con un producto sin resolver no se confirma nada.
	if _, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions); !isInvalid(err) {
		t.Fatalf("debe pedir resolver: %v", err)
	}
	conf, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions[:3])
	if err != nil || len(conf.Created) != 3 {
		t.Fatalf("confirmar: %v %+v", err, conf)
	}
	var j models.ServiceItem
	db.First(&j, "id = ?", jugo.ID)
	if !j.SoldOut {
		t.Fatal("el jugo debe quedar agotado")
	}
	ov, _ := svc.Inventory(owner.ID, ent.ID)
	c := findInv(t, ov, "Camisa")
	if c.Qty != 12 || c.Ref != camisa.ID {
		t.Fatalf("camisas: %+v", c)
	}
	if findInv(t, ov, "Lechuga").Level != "low" {
		t.Fatal("lechuga queda poca")
	}
}

func TestIngredients(t *testing.T) {
	db, svc, owner, other, ent := ledgerFixture(t)
	bandeja := stockProduct(t, db, ent, "Bandeja Paisa", 25000)
	carne, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Carne"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetIngredients(other.ID, ent.ID, bandeja.ID, []uuid.UUID{carne.ID}); err != ErrLedgerForbidden {
		t.Fatalf("otro usuario: %v", err)
	}
	if _, err := svc.SetIngredients(owner.ID, ent.ID, bandeja.ID, []uuid.UUID{uuid.New()}); !isInvalid(err) {
		t.Fatalf("insumo inexistente: %v", err)
	}
	ov, err := svc.SetIngredients(owner.ID, ent.ID, bandeja.ID, []uuid.UUID{carne.ID, carne.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := findInv(t, ov, "Bandeja Paisa").Ingredients; len(got) != 1 || got[0].Name != "Carne" {
		t.Fatalf("ingredientes: %+v", got)
	}
	if got := findInv(t, ov, "Carne").UsedIn; len(got) != 1 || got[0].Name != "Bandeja Paisa" {
		t.Fatalf("usado en: %+v", got)
	}
	if err := svc.SetLevel(owner.ID, ent.ID, carne.ID, "out", uuid.New()); err != nil {
		t.Fatal(err)
	}
	ins, _ := svc.Insights(owner.ID, ent.ID)
	found := false
	for _, i := range ins {
		if i.Kind == "out" && strings.Contains(i.Body, "Bandeja Paisa") {
			found = true
		}
	}
	if !found {
		t.Fatalf("el aviso debe nombrar el plato afectado: %+v", ins)
	}
	if err := svc.ArchiveSupply(owner.ID, ent.ID, carne.ID); err != nil {
		t.Fatal(err)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if len(findInv(t, ov, "Bandeja Paisa").Ingredients) != 0 {
		t.Fatal("al quitar el insumo se quita del plato")
	}
}
