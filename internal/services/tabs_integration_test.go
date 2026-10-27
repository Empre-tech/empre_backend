package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// Fase 8: una cuenta se va llenando y al cobrarla es una sola venta; nada se duplica con reenvíos.
func TestOpenTabs(t *testing.T) {
	db, svc, owner, other, ent := ledgerFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()
	corte, _ := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Corte", Kind: "service", Price: intp(20000), DurationMin: intp(30)}, now)
	gaseosa, _ := cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Gaseosa", Kind: "product", Price: intp(3000)}, now)
	svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: gaseosa.ID, Counted: fp(10)})

	if _, err := svc.OpenTab(other.ID, ent.ID, OpenTabInput{ID: uuid.New(), Name: "Juan"}); err != ErrLedgerForbidden {
		t.Fatalf("otro usuario: %v", err)
	}
	if _, err := svc.OpenTab(owner.ID, ent.ID, OpenTabInput{ID: uuid.New(), Name: "  "}); !isInvalid(err) {
		t.Fatalf("sin nombre: %v", err)
	}
	tabID := uuid.New()
	tab, err := svc.OpenTab(owner.ID, ent.ID, OpenTabInput{ID: tabID, Name: "Juan"})
	if err != nil || tab.ID != tabID || tab.Status != models.TabOpen {
		t.Fatalf("abrir: %v %+v", err, tab)
	}
	again, _ := svc.OpenTab(owner.ID, ent.ID, OpenTabInput{ID: tabID, Name: "Juan"})
	if again.ID != tabID {
		t.Fatal("abrir otra vez (reenvío) devuelve la misma cuenta")
	}

	l1, l2 := uuid.New(), uuid.New()
	lines := []TabLineInput{{ID: l1, ItemID: &corte.ID, Qty: 1}, {ID: l2, ItemID: &gaseosa.ID, Qty: 2}}
	tab, err = svc.AddToTab(owner.ID, ent.ID, tabID, lines)
	if err != nil || tab.Total != 26000 {
		t.Fatalf("corte + 2 gaseosas = 26.000: %v %+v", err, tab)
	}
	tab, _ = svc.AddToTab(owner.ID, ent.ID, tabID, lines) // reenvío
	if tab.Total != 26000 || len(tab.Lines) != 2 {
		t.Fatalf("el reenvío no duplica: %+v", tab)
	}
	// El precio queda fijo al pedir: cambiar el menú no altera la cuenta.
	cat.Update(ent.ID, gaseosa.ID, owner.ID, ServiceInput{Name: "Gaseosa", Price: intp(3500)}, now)
	l3 := uuid.New()
	tab, _ = svc.AddToTab(owner.ID, ent.ID, tabID, []TabLineInput{{ID: l3, ItemID: &gaseosa.ID, Qty: 1}})
	if tab.Total != 29500 {
		t.Fatalf("la gaseosa nueva va al precio nuevo, las de antes no: %d", tab.Total)
	}
	tab, _ = svc.RemoveFromTab(owner.ID, ent.ID, tabID, l3)
	if tab.Total != 26000 {
		t.Fatalf("quitar la última: %d", tab.Total)
	}
	tabs, _ := svc.ListTabs(owner.ID, ent.ID)
	if len(tabs) != 1 {
		t.Fatalf("una cuenta abierta: %d", len(tabs))
	}

	// Mientras está abierta no mueve inventario; al cobrar, sí, y queda una sola venta.
	ov, _ := svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Gaseosa").Qty; q != 10 {
		t.Fatalf("abierta no descuenta: %v", q)
	}
	saleID := uuid.New()
	tab, err = svc.PayTab(owner.ID, ent.ID, tabID, PayTabInput{SaleID: saleID, Method: "cash", Tip: 2000})
	if err != nil || tab.Status != models.TabPaid || tab.SaleID == nil || tab.Tip != 2000 {
		t.Fatalf("cobrar: %v %+v", err, tab)
	}
	if _, err := svc.PayTab(owner.ID, ent.ID, tabID, PayTabInput{SaleID: saleID, Method: "cash"}); err != nil {
		t.Fatalf("cobrar otra vez (reenvío) no falla: %v", err)
	}
	var sales []models.Sale
	db.Where("entity_id = ? AND status = ?", ent.ID, models.LedgerActive).Find(&sales)
	if len(sales) != 1 || sales[0].Total != 26000 {
		t.Fatalf("una sola venta de 26.000 (la propina va aparte): %+v", sales)
	}
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if q := findInv(t, ov, "Gaseosa").Qty; q != 8 {
		t.Fatalf("al cobrar bajan 2 gaseosas: %v", q)
	}
	if _, err := svc.AddToTab(owner.ID, ent.ID, tabID, []TabLineInput{{ID: uuid.New(), ItemID: &gaseosa.ID, Qty: 1}}); !isInvalid(err) {
		t.Fatalf("una cuenta cobrada no recibe más: %v", err)
	}

	// A fiado: necesita el cliente y queda en sus fiados.
	ana, _, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Kind: "customer", Name: "Ana"})
	t2 := uuid.New()
	svc.OpenTab(owner.ID, ent.ID, OpenTabInput{ID: t2, Name: "Ana"})
	svc.AddToTab(owner.ID, ent.ID, t2, []TabLineInput{{ID: uuid.New(), ItemID: &corte.ID, Qty: 1}})
	if _, err := svc.PayTab(owner.ID, ent.ID, t2, PayTabInput{SaleID: uuid.New(), Method: "credit"}); !isInvalid(err) {
		t.Fatalf("fiado sin cliente: %v", err)
	}
	if _, err := svc.PayTab(owner.ID, ent.ID, t2, PayTabInput{SaleID: uuid.New(), Method: "credit", PartyID: &ana.ID}); err != nil {
		t.Fatal(err)
	}
	owing, _ := svc.ListParties(owner.ID, ent.ID, "customer", true)
	if len(owing) != 1 || owing[0].Balance != 20000 {
		t.Fatalf("Ana debe el corte: %+v", owing)
	}

	// Cancelar: no toca plata ni inventario. Vacía no se cobra.
	t3 := uuid.New()
	svc.OpenTab(owner.ID, ent.ID, OpenTabInput{ID: t3, Name: "Mesa 3"})
	if _, err := svc.PayTab(owner.ID, ent.ID, t3, PayTabInput{SaleID: uuid.New(), Method: "cash"}); !isInvalid(err) {
		t.Fatalf("vacía no se cobra: %v", err)
	}
	if tab, err := svc.CancelTab(owner.ID, ent.ID, t3, "se fueron"); err != nil || tab.Status != models.TabCancelled {
		t.Fatalf("cancelar: %v %+v", err, tab)
	}
	if _, err := svc.CancelTab(owner.ID, ent.ID, t3, "otra vez"); err != nil {
		t.Fatalf("cancelar dos veces no falla: %v", err)
	}

	// Desde una cita: arranca con el servicio y el nombre del cliente.
	start := now.Add(time.Hour)
	appt := models.Appointment{EntityID: ent.ID, Kind: "booking", CustomerID: &other.ID, ServiceID: &corte.ID, StartsAt: start, EndsAt: start.Add(30 * time.Minute), BlocksUntil: start.Add(30 * time.Minute), Status: models.ApptConfirmed}
	if err := db.Create(&appt).Error; err != nil {
		t.Fatal(err)
	}
	t4, err := svc.OpenTab(owner.ID, ent.ID, OpenTabInput{ID: uuid.New(), AppointmentID: &appt.ID})
	if err != nil || t4.Name != "Otro" || len(t4.Lines) != 1 || t4.Total != 20000 {
		t.Fatalf("cuenta desde la cita: %v %+v", err, t4)
	}
}

// Voz: «mesa 3: dos bandejas» abre la mesa y agrega; «cobra la mesa 3» la cierra; dentro de una cuenta, «una
// gaseosa» va a esa cuenta.
func TestVoiceTabs(t *testing.T) {
	TabsEnabled = true
	defer func() { TabsEnabled = false }()
	db, svc, owner, _, ent := ledgerFixture(t)
	cat := NewCatalogService(db)
	now := time.Now()
	cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Bandeja paisa", Kind: "product", Price: intp(25000)}, now)
	cat.Create(ent.ID, owner.ID, ServiceInput{Name: "Limonada", Kind: "product", Price: intp(5000)}, now)
	fm := &fakeModel{on: true}
	svc.SetVoiceModel(fm)

	fm.reply = `{"actions":[{"type":"tab_add","tab":"mesa 3","items":[{"name":"bandeja paisa","qty":2},{"name":"limonada","qty":1}]}]}`
	res, err := svc.ParseVoice(owner.ID, ent.ID, "mesa 3 dos bandejas y una limonada")
	if err != nil || len(res.Actions) != 1 {
		t.Fatalf("interpretar: %v %+v", err, res)
	}
	a := res.Actions[0]
	if a.Type != ActionTabAdd || !a.NewTab || a.TabName != "mesa 3" || a.Amount != 55000 || len(a.Warnings) != 0 {
		t.Fatalf("agregar a una mesa nueva: %+v", a)
	}
	if _, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions); err != nil {
		t.Fatal(err)
	}
	tabs, _ := svc.ListTabs(owner.ID, ent.ID)
	if len(tabs) != 1 || tabs[0].Total != 55000 {
		t.Fatalf("la mesa 3 queda abierta con 55.000: %+v", tabs)
	}

	// Desde dentro de la cuenta, una venta va a la cuenta.
	fm.reply = `{"actions":[{"type":"sale","items":[{"name":"limonada","qty":1}]}]}`
	res, _ = svc.ParseVoiceIn(owner.ID, ent.ID, "otra limonada", &tabs[0].ID)
	if res.Actions[0].Type != ActionTabAdd || res.Actions[0].TabID == nil || *res.Actions[0].TabID != tabs[0].ID {
		t.Fatalf("dentro de la cuenta, va a la cuenta: %+v", res.Actions[0])
	}
	svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions)

	fm.reply = `{"actions":[{"type":"tab_pay","tab":"la mesa 3","method":"transfer"}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "cobra la mesa 3 por nequi")
	if p := res.Actions[0]; p.TabID == nil || p.Amount != 60000 || p.Method != "transfer" {
		t.Fatalf("cobrar la mesa: %+v", p)
	}
	if _, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions); err != nil {
		t.Fatal(err)
	}
	tabs, _ = svc.ListTabs(owner.ID, ent.ID)
	var sales []models.Sale
	db.Where("entity_id = ?", ent.ID).Find(&sales)
	if len(tabs) != 0 || len(sales) != 1 || sales[0].Total != 60000 || sales[0].Source != models.SourceVoice {
		t.Fatalf("la mesa se cobra como una venta por voz: %d abiertas, %+v", len(tabs), sales)
	}
}

// Apagadas: «mesa 3: dos bandejas» se registra como venta normal y «cobra la mesa» se ignora.
func TestVoiceTabsOff(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	NewCatalogService(db).Create(ent.ID, owner.ID, ServiceInput{Name: "Bandeja paisa", Kind: "product", Price: intp(25000)}, time.Now())
	fm := &fakeModel{on: true}
	svc.SetVoiceModel(fm)
	fm.reply = `{"actions":[{"type":"tab_add","tab":"mesa 3","items":[{"name":"bandeja paisa","qty":2}]},{"type":"tab_pay","tab":"mesa 3"}]}`
	res, err := svc.ParseVoice(owner.ID, ent.ID, "mesa 3 dos bandejas y cobra la mesa 3")
	if err != nil || len(res.Actions) != 1 || res.Actions[0].Type != ActionSale || res.Actions[0].Amount != 50000 {
		t.Fatalf("con cuentas apagadas es una venta: %v %+v", err, res.Actions)
	}
}
