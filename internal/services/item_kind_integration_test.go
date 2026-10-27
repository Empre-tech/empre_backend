package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/database"
	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// Cada ítem dice si es servicio o producto: un servicio no lleva inventario y cambiar de tipo no borra nada.
func TestItemKinds(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()

	// Un negocio de servicios agrega servicios por defecto; una tienda, productos.
	db.Model(&models.Entity{}).Where("id = ?", ent.ID).Update("business_profile", "service")
	corte, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Corte"}, now)
	if err != nil || corte.Kind != models.ItemService || !corte.KindChecked {
		t.Fatalf("por defecto, servicio: %v %+v", err, corte)
	}
	cera, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Cera para el pelo", Kind: "product"}, now)
	if err != nil || cera.Kind != models.ItemProduct {
		t.Fatalf("el dueño elige producto: %v %+v", err, cera)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Gel", Kind: "product", DurationMin: intp(30)}, now); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("un producto no lleva duración: %v", err)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Raro", Kind: "cosa"}, now); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("tipo inválido: %v", err)
	}

	// Un servicio no acepta conteos ni compras para inventario.
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: corte.ID, Counted: fp(3)}); !isInvalid(err) {
		t.Fatalf("contar un servicio debe fallar: %v", err)
	}
	if _, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: uuid.New(), Category: "mercancia", Method: "cash",
		Lines: []PurchaseLine{{ItemID: &corte.ID, Qty: 1, UnitCost: 100}}}); !isInvalid(err) {
		t.Fatalf("comprar un servicio para inventario debe fallar: %v", err)
	}

	// Producto con 5 unidades → servicio: el inventario se archiva y la lista lo muestra sin control.
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: cera.ID, Counted: fp(5)}); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.SetKind(ent.ID, cera.ID, owner.ID, "service"); err != nil {
		t.Fatal(err)
	}
	ov, _ := svc.Inventory(owner.ID, ent.ID)
	if it := findInv(t, ov, "Cera para el pelo"); !it.IsService || it.Mode != "none" {
		t.Fatalf("como servicio no lleva inventario: %+v", it)
	}
	// … y de vuelta a producto: recupera sus 5.
	back, err := cat.SetKind(ent.ID, cera.ID, owner.ID, "product")
	if err != nil || back.Kind != models.ItemProduct {
		t.Fatalf("volver a producto: %v %+v", err, back)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if it := findInv(t, ov, "Cera para el pelo"); it.IsService || it.Qty != 5 {
		t.Fatalf("vuelve con su historia: %+v", it)
	}
	if _, err := cat.SetKind(ent.ID, corte.ID, uuid.New(), "product"); !errors.Is(err, ErrCatalogNotOwner) {
		t.Fatalf("otro usuario: %v", err)
	}

	// La migración clasifica los ítems viejos (sin tipo) y deja sin confirmar los que no tienen señal clara.
	old1 := models.ServiceItem{EntityID: ent.ID, Name: "Tinte", Status: models.ServiceActive, DurationMin: intp(60)}
	old2 := models.ServiceItem{EntityID: ent.ID, Name: "Mensualidad", Status: models.ServiceActive}
	db.Create(&old1)
	db.Create(&old2)
	if err := database.BackfillItemKinds(db); err != nil {
		t.Fatal(err)
	}
	db.First(&old1, "id = ?", old1.ID)
	db.First(&old2, "id = ?", old2.ID)
	if old1.Kind != "service" || !old1.KindChecked {
		t.Fatalf("con duración: servicio confirmado: %+v", old1)
	}
	if old2.Kind != "service" || old2.KindChecked {
		t.Fatalf("sin señal, en negocio de servicios: servicio sin confirmar: %+v", old2)
	}
	if err := cat.ConfirmKinds(ent.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	db.First(&old2, "id = ?", old2.ID)
	if !old2.KindChecked {
		t.Fatal("confirmar deja todo revisado")
	}
}

// Fase 3: cualquier ítem tiene insumos, y un producto que se vende también puede ser insumo (una sola existencia).
func TestSuppliesForAnyItem(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()
	tinteSrv, _ := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Tinte completo", Kind: "service", DurationMin: intp(90)}, now)
	champu, _ := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Champú", Kind: "product", Price: intp(18000)}, now)
	tinte, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Tinte negro"})
	if err != nil {
		t.Fatal(err)
	}

	// Un producto sin «también lo uso» no se puede elegir como insumo.
	if _, err := svc.SetIngredients(owner.ID, ent.ID, tinteSrv.ID, []uuid.UUID{champu.ID}); !isInvalid(err) {
		t.Fatalf("sin marcar, el producto no es insumo: %v", err)
	}
	yes := true
	if _, err := svc.UpdateStockSettings(owner.ID, ent.ID, champu.ID, ItemSettings{AlsoSupply: &yes}); err != nil {
		t.Fatal(err)
	}
	ov, err := svc.SetIngredients(owner.ID, ent.ID, tinteSrv.ID, []uuid.UUID{tinte.ID, champu.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := findInv(t, ov, "Tinte completo").Ingredients; len(got) != 2 {
		t.Fatalf("el servicio usa tinte y champú: %+v", got)
	}
	ch := findInv(t, ov, "Champú")
	if !ch.AlsoSupply || len(ch.UsedIn) != 1 || ch.UsedIn[0].Name != "Tinte completo" {
		t.Fatalf("el champú se vende y se usa: %+v", ch)
	}
	if _, err := svc.SetIngredients(owner.ID, ent.ID, champu.ID, []uuid.UUID{champu.ID}); !isInvalid(err) {
		t.Fatalf("un ítem no se usa a sí mismo: %v", err)
	}

	// El aviso nombra el servicio afectado.
	if err := svc.SetLevel(owner.ID, ent.ID, tinte.ID, "low", uuid.New()); err != nil {
		t.Fatal(err)
	}
	ins, _ := svc.Insights(owner.ID, ent.ID)
	found := false
	for _, i := range ins {
		if i.Kind == "low" && strings.Contains(i.Body, "Tinte completo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("el aviso debe nombrar el servicio: %+v", ins)
	}

	// Apagar «también lo uso» lo quita de donde se usaba.
	no := false
	if _, err := svc.UpdateStockSettings(owner.ID, ent.ID, champu.ID, ItemSettings{AlsoSupply: &no}); err != nil {
		t.Fatal(err)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if got := findInv(t, ov, "Tinte completo").Ingredients; len(got) != 1 {
		t.Fatalf("queda solo el tinte: %+v", got)
	}
}

// Fase 5: con cantidad por unidad, vender descuenta el insumo; anular lo devuelve; el reenvío no descuenta dos veces.
func TestRecipes(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	cat := NewCatalogService(db)
	bandeja, _ := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Bandeja paisa", Kind: "product", Price: intp(25000)}, time.Now())
	carne, _ := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Carne molida"})
	arroz, _ := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Arroz"})
	exact := "exact"
	for _, s := range []uuid.UUID{carne.ID, arroz.ID} {
		if _, err := svc.UpdateStockSettings(owner.ID, ent.ID, s, ItemSettings{Mode: &exact}); err != nil {
			t.Fatal(err)
		}
	}
	svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: carne.ID, Counted: fp(1000)}) // 1.000 g
	svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: arroz.ID, Counted: fp(5000)})

	if _, err := svc.SetRecipe(owner.ID, ent.ID, bandeja.ID, []IngredientInput{{Ref: carne.ID, Qty: fp(0)}}); !isInvalid(err) {
		t.Fatalf("cantidad 0 no vale: %v", err)
	}
	ov, err := svc.SetRecipe(owner.ID, ent.ID, bandeja.ID, []IngredientInput{{Ref: carne.ID, Qty: fp(150)}, {Ref: arroz.ID, Qty: fp(200)}})
	if err != nil {
		t.Fatal(err)
	}
	b := findInv(t, ov, "Bandeja paisa")
	if b.Servings == nil || *b.Servings != 6 || b.Limit != "Carne molida" {
		t.Fatalf("1.000 g de carne a 150 g: alcanza para 6, limita la carne: %+v", b)
	}

	sid := uuid.New()
	sale := SaleInput{ClientID: sid, Method: "cash", Lines: []LineInput{{ItemID: &bandeja.ID, Qty: 2}}}
	if _, _, err := svc.CreateSale(owner.ID, ent.ID, sale); err != nil {
		t.Fatal(err)
	}
	_, _, _ = svc.CreateSale(owner.ID, ent.ID, sale) // reenvío
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Carne molida").Qty; q != 700 {
		t.Fatalf("2 bandejas × 150 g: quedan 700, got %v", q)
	}
	if q := findInv(t, ov, "Arroz").Qty; q != 4600 {
		t.Fatalf("arroz: quedan 4.600, got %v", q)
	}
	if _, err := svc.VoidSale(owner.ID, ent.ID, sid, "error"); err != nil {
		t.Fatal(err)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Carne molida").Qty; q != 1000 {
		t.Fatalf("anular devuelve la carne: %v", q)
	}

	// Un insumo en semáforo no se descuenta: se lleva a mano.
	svc.SetRecipe(owner.ID, ent.ID, bandeja.ID, []IngredientInput{{Ref: carne.ID, Qty: fp(150)}})
	light := "light"
	svc.UpdateStockSettings(owner.ID, ent.ID, carne.ID, ItemSettings{Mode: &light})
	if _, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "cash", Lines: []LineInput{{ItemID: &bandeja.ID, Qty: 1}}}); err != nil {
		t.Fatal(err)
	}
	var moves int64
	db.Model(&models.StockMovement{}).Where("stock_item_id = ? AND kind = ?", carne.ID, models.MoveUse).Count(&moves)
	if moves != 1 { // solo la venta anulada de antes
		t.Fatalf("en semáforo no se descuenta: %d movimientos de uso", moves)
	}
}

// Fase 7: variantes con existencia propia; el producto agrupa, no se vende ni se cuenta.
func TestVariants(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()
	prot, _ := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Proteína 2 lb", Kind: "product", Price: intp(120000), Section: "Suplementos"}, now)
	cost := 95000
	if err := svc.SetItemCost(owner.ID, ent.ID, prot.ID, &cost); err != nil {
		t.Fatal(err)
	}
	// Antes de las variantes tenía 3 contadas: al agruparse, su existencia se archiva.
	svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: prot.ID, Counted: fp(3)})

	choc, err := cat.Create(ent.ID, owner.ID, ServiceInput{ParentID: &prot.ID, VariantLabel: " Chocolate "}, now)
	if err != nil || choc.Name != "Proteína 2 lb · Chocolate" || choc.VariantLabel != "Chocolate" || choc.Section != "Suplementos" {
		t.Fatalf("variante: %v %+v", err, choc)
	}
	vain, _ := cat.Create(ent.ID, owner.ID, ServiceInput{ParentID: &prot.ID, VariantLabel: "Vainilla", Price: intp(125000)}, now)
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{ParentID: &choc.ID, VariantLabel: "Doble"}, now); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("una variante no tiene variantes: %v", err)
	}
	if _, err := cat.Create(ent.ID, owner.ID, ServiceInput{ParentID: &prot.ID, VariantLabel: " "}, now); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("variante sin nombre: %v", err)
	}

	// El producto no se cuenta ni se vende; cada variante sí, con el precio heredado o el suyo.
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: prot.ID, Counted: fp(2)}); err == nil {
		t.Fatal("el producto con variantes no se cuenta")
	}
	if _, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "cash", Lines: []LineInput{{ItemID: &prot.ID, Qty: 1}}}); !isInvalid(err) {
		t.Fatalf("vender el producto debe pedir la variante: %v", err)
	}
	svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: choc.ID, Counted: fp(2)})
	svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: vain.ID, Counted: fp(1)})
	sale, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "cash", Lines: []LineInput{{ItemID: &choc.ID, Qty: 1}}})
	if err != nil || sale.Total != 120000 {
		t.Fatalf("la variante sin precio usa el del producto: %v %+v", err, sale)
	}
	items, _ := svc.ListItems(owner.ID, ent.ID)
	for _, it := range items {
		if it.ID == prot.ID {
			t.Fatal("el producto con variantes no sale para vender")
		}
		if it.ID == choc.ID && (it.Price == nil || *it.Price != 120000 || it.Cost == nil || *it.Cost != 95000) {
			t.Fatalf("precio y costo heredados: %+v", it)
		}
	}

	// Agotar vainilla no agota el producto; agotar las dos sí.
	svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "cash", Lines: []LineInput{{ItemID: &vain.ID, Qty: 1}}})
	var p models.ServiceItem
	db.First(&p, "id = ?", prot.ID)
	if p.SoldOut {
		t.Fatal("con chocolate disponible, el producto no está agotado")
	}
	svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "cash", Lines: []LineInput{{ItemID: &choc.ID, Qty: 1}}})
	p = models.ServiceItem{}
	db.First(&p, "id = ?", prot.ID)
	if !p.SoldOut {
		t.Fatal("sin ninguna variante, el producto se ve agotado")
	}
	ov, _ := svc.Inventory(owner.ID, ent.ID)
	if g := findInv(t, ov, "Proteína 2 lb"); g.Variants != 2 || g.Mode != "none" {
		t.Fatalf("el producto agrupa: %+v", g)
	}
	if v := findInv(t, ov, "Proteína 2 lb · Vainilla"); v.ParentID == nil || v.VariantLabel != "Vainilla" || v.Price == nil || *v.Price != 125000 {
		t.Fatalf("la variante con precio propio: %+v", v)
	}

	// Renombrar el producto renombra sus variantes; quitarlo las quita.
	if _, err := cat.Update(ent.ID, prot.ID, owner.ID, ServiceInput{Name: "Whey 2 lb", Price: intp(120000), Section: "Suplementos"}, now); err != nil {
		t.Fatal(err)
	}
	var cv models.ServiceItem
	db.First(&cv, "id = ?", choc.ID)
	if cv.Name != "Whey 2 lb · Chocolate" {
		t.Fatalf("la variante sigue el nombre: %q", cv.Name)
	}
	if err := cat.Retire(ent.ID, prot.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.ServiceItem{}).Where("parent_id = ? AND status = ?", prot.ID, models.ServiceActive).Count(&n)
	if n != 0 {
		t.Fatalf("quitar el producto quita sus variantes: %d activas", n)
	}
}
