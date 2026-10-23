package services

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func ledgerFixture(t *testing.T) (*gorm.DB, *LedgerService, models.User, models.User, models.Entity) {
	db := testDB(t)
	owner := models.User{Name: "Marta", Email: "marta@x.co", PasswordHash: "x"}
	other := models.User{Name: "Otro", Email: "otro@x.co", PasswordHash: "x"}
	for _, u := range []*models.User{&owner, &other} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	cat := models.Category{Name: "Comida"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	ent := models.Entity{OwnerID: owner.ID, Name: "Panadería Marta", CategoryID: cat.ID}
	if err := db.Omit("Owner", "Category").Create(&ent).Error; err != nil {
		t.Fatal(err)
	}
	return db, NewLedgerService(db), owner, other, ent
}

func ip(n int) *int { return &n }

func isInvalid(err error) bool {
	var inv *InvalidError
	return errors.As(err, &inv)
}

func TestLedgerSalesAndDebts(t *testing.T) {
	db, svc, owner, other, ent := ledgerFixture(t)

	// Solo el dueño.
	if _, _, err := svc.CreateSale(other.ID, ent.ID, SaleInput{ClientID: uuid.New(), Total: ip(5000), Method: "cash"}); err != ErrLedgerForbidden {
		t.Fatalf("otro usuario debe recibir forbidden, got %v", err)
	}

	// Venta simple + reenvío idempotente.
	cid := uuid.New()
	s1, isNew, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: cid, Total: ip(50000), Method: "cash"})
	if err != nil || !isNew {
		t.Fatalf("venta: %v new=%v", err, isNew)
	}
	if s1.ID != cid {
		t.Fatalf("el id de la venta debe ser su client_id")
	}
	s1b, isNew, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: cid, Total: ip(50000), Method: "cash"})
	if err != nil || isNew || s1b.ID != s1.ID {
		t.Fatalf("el reenvío debe devolver la misma venta: %v new=%v", err, isNew)
	}
	var n int64
	db.Model(&models.Sale{}).Count(&n)
	if n != 1 {
		t.Fatalf("esperaba 1 venta, hay %d", n)
	}

	// Validaciones.
	bad := []SaleInput{
		{ClientID: uuid.New(), Total: ip(0), Method: "cash"},
		{ClientID: uuid.New(), Total: ip(200_000_000), Method: "cash"},
		{ClientID: uuid.New(), Total: ip(1000), Method: "bitcoin"},
		{ClientID: uuid.New(), Method: "cash"},
		{ClientID: uuid.New(), Total: ip(1000), Method: "credit"}, // fiado sin cliente
		{Total: ip(1000), Method: "cash"},                         // sin client_id
	}
	future := nowCO().Add(48 * time.Hour)
	bad = append(bad, SaleInput{ClientID: uuid.New(), Total: ip(1000), Method: "cash", OccurredAt: &future})
	for i, in := range bad {
		if _, _, err := svc.CreateSale(owner.ID, ent.ID, in); !isInvalid(err) {
			t.Errorf("caso %d: esperaba InvalidError, got %v", i, err)
		}
	}

	// Catálogo con costo: la venta guarda el costo del momento.
	item := models.ServiceItem{EntityID: ent.ID, Name: "Pan de bono", Price: ip(2000), Status: models.ServiceActive}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SetItemCost(owner.ID, ent.ID, item.ID, ip(800)); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetItemCost(other.ID, ent.ID, item.ID, ip(1)); err != ErrLedgerForbidden {
		t.Fatalf("costo: otro usuario %v", err)
	}
	s2, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Method: "transfer",
		Lines: []LineInput{{ItemID: &item.ID, Qty: 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if s2.Total != 6000 || s2.Lines[0].UnitCost == nil || *s2.Lines[0].UnitCost != 800 {
		t.Fatalf("venta con líneas mal: %+v", s2)
	}
	_ = svc.SetItemCost(owner.ID, ent.ID, item.ID, ip(1500))
	var stored models.SaleLine
	db.First(&stored, "sale_id = ?", s2.ID)
	if *stored.UnitCost != 800 {
		t.Fatalf("cambiar el costo no debe reescribir la historia: %d", *stored.UnitCost)
	}

	// Fiado.
	pc := uuid.New()
	cust, _, err := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: pc, Name: "Don Jorge"})
	if err != nil {
		t.Fatal(err)
	}
	if again, isNew, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: pc, Name: "Don Jorge"}); isNew || again.ID != cust.ID {
		t.Fatal("crear tercero dos veces con el mismo client_id debe ser idempotente")
	}
	credit, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), Total: ip(30000), Method: "credit", PartyID: &cust.ID})
	if err != nil {
		t.Fatal(err)
	}
	owing, _ := svc.ListParties(owner.ID, ent.ID, "customer", true)
	if len(owing) != 1 || owing[0].Balance != 30000 {
		t.Fatalf("saldo esperado 30000, got %+v", owing)
	}
	payID := uuid.New()
	if _, isNew, err := svc.CreatePayment(owner.ID, ent.ID, PaymentInput{ClientID: payID, PartyID: cust.ID, Amount: 10000, Method: "cash"}); err != nil || !isNew {
		t.Fatalf("abono: %v", err)
	}
	if _, isNew, err := svc.CreatePayment(owner.ID, ent.ID, PaymentInput{ClientID: payID, PartyID: cust.ID, Amount: 10000, Method: "cash"}); err != nil || isNew {
		t.Fatalf("reenvío de abono: %v new=%v", err, isNew)
	}
	if _, _, err := svc.CreatePayment(owner.ID, ent.ID, PaymentInput{ClientID: uuid.New(), PartyID: cust.ID, Amount: 25000, Method: "cash"}); !isInvalid(err) {
		t.Fatalf("un abono mayor a la deuda debe rechazarse: %v", err)
	}
	owing, _ = svc.ListParties(owner.ID, ent.ID, "customer", true)
	if owing[0].Balance != 20000 {
		t.Fatalf("saldo tras abono: %d", owing[0].Balance)
	}

	// Anular el fiado baja la deuda; anular dos veces no rompe.
	if _, err := svc.VoidSale(owner.ID, ent.ID, credit.ID, "se equivocó"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.VoidSale(owner.ID, ent.ID, credit.ID, "otra vez"); err != nil {
		t.Fatal(err)
	}

	// Gastos: compra a crédito exige proveedor.
	if _, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: uuid.New(), Amount: 40000, Method: "credit"}); !isInvalid(err) {
		t.Fatalf("crédito sin proveedor: %v", err)
	}
	sup, _, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Kind: "supplier", Name: "Harinas del Valle"})
	if _, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: uuid.New(), Amount: 40000, Method: "credit", PartyID: &sup.ID, Category: "mercancia"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: uuid.New(), Amount: 15000, Method: "cash", Category: "servicios"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: uuid.New(), Amount: 15000, Method: "cash", Category: "inventada"}); !isInvalid(err) {
		t.Fatalf("categoría inválida: %v", err)
	}
	// Pago al proveedor: dirección payable.
	p, _, err := svc.CreatePayment(owner.ID, ent.ID, PaymentInput{ClientID: uuid.New(), PartyID: sup.ID, Amount: 10000, Method: "transfer"})
	if err != nil || p.Direction != models.DebtPayable {
		t.Fatalf("pago a proveedor: %v %+v", err, p)
	}

	// Resumen del día (la venta anulada no cuenta).
	sum, err := svc.Summary(owner.ID, ent.ID, "day", nowCO())
	if err != nil {
		t.Fatal(err)
	}
	if sum.SalesTotal != 56000 || sum.SalesCount != 2 {
		t.Fatalf("ventas: %+v", sum)
	}
	if sum.ExpensesTotal != 55000 || sum.Profit != 1000 {
		t.Fatalf("gastos/utilidad: %d %d", sum.ExpensesTotal, sum.Profit)
	}
	if sum.ByMethod.Cash != 50000 || sum.ByMethod.Transfer != 6000 || sum.ByMethod.Credit != 0 {
		t.Fatalf("por medio: %+v", sum.ByMethod)
	}
	// Caja: entra 50.000 + 6.000 + 10.000 abonados; sale 15.000 gasto + 10.000 al proveedor. La compra a crédito no sale.
	if sum.CashIn != 66000 || sum.CashOut != 25000 {
		t.Fatalf("caja in=%d out=%d", sum.CashIn, sum.CashOut)
	}
	if sum.Receivable != 0 || sum.Payable != 30000 {
		t.Fatalf("deudas: por cobrar %d, por pagar %d", sum.Receivable, sum.Payable)
	}
	if sum.GrossMargin != 3600 || sum.MarginCoverage != 100 {
		t.Fatalf("margen %d cobertura %d", sum.GrossMargin, sum.MarginCoverage)
	}
	if len(sum.TopItems) != 1 || sum.TopItems[0].Name != "Pan de bono" {
		t.Fatalf("top: %+v", sum.TopItems)
	}

	// Exportar.
	csv, err := svc.ExportCSV(owner.ID, ent.ID, sum.From, sum.To)
	if err != nil || len(csv) < 50 {
		t.Fatalf("csv: %v len=%d", err, len(csv))
	}
}

// ---- voz ----

type fakeModel struct {
	on    bool
	reply string
	calls int
}

func (f *fakeModel) Enabled() bool { return f.on }
func (f *fakeModel) Chat(system string, msgs []AIMessage, tools []AITool) (*AIReply, error) {
	f.calls++
	return &AIReply{ToolCalls: []AIToolCall{{Name: voiceToolName, Input: json.RawMessage(f.reply)}}}, nil
}

func TestLedgerVoice(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)

	if _, err := svc.ParseVoice(owner.ID, ent.ID, "vendí algo"); err != ErrVoiceDisabled {
		t.Fatalf("sin modelo: %v", err)
	}

	item := models.ServiceItem{EntityID: ent.ID, Name: "Empanada", Price: ip(2500), Status: models.ServiceActive}
	db.Create(&item)
	cust, _, _ := svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Name: "Carlos Pérez"})
	svc.CreateParty(owner.ID, ent.ID, PartyInput{ClientID: uuid.New(), Name: "Carlos Gómez"})

	fm := &fakeModel{on: true}
	svc.SetVoiceModel(fm)

	// La IA se equivoca por un cero (2.500.000) pero el dueño dijo «veinticinco mil»: el código corrige.
	fm.reply = `{"actions":[
		{"type":"sale","amount_spoken":"veinticinco mil","amount":2500000,"method":"cash"},
		{"type":"sale","amount_spoken":"treinta mil","amount":30000,"method":"credit","party_name":"Marta nueva clienta"},
		{"type":"sale","amount_spoken":"diez mil","amount":10000,"method":"credit","party_name":"Carlos"},
		{"type":"expense","amount_spoken":"15.000","amount":15000,"category":"nomina"}
	]}`
	res, err := svc.ParseVoice(owner.ID, ent.ID, "vendí veinticinco mil, fiado a una nueva clienta treinta mil, fiado a Carlos diez mil y gasté 15.000")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 4 {
		t.Fatalf("acciones: %+v", res.Actions)
	}
	a0 := res.Actions[0]
	if a0.Amount != 25000 || a0.Words != "veinticinco mil pesos" || len(a0.Warnings) == 0 {
		t.Fatalf("el monto debe corregirse con aviso: %+v", a0)
	}
	if !res.Actions[1].NewParty {
		t.Fatalf("cliente nuevo: %+v", res.Actions[1])
	}
	if len(res.Actions[2].Candidates) != 2 || res.Actions[2].PartyID != nil {
		t.Fatalf("«Carlos» es ambiguo: %+v", res.Actions[2])
	}
	var sales int64
	db.Model(&models.Sale{}).Count(&sales)
	if sales != 0 {
		t.Fatal("interpretar no debe crear ventas")
	}

	// El dueño resuelve al cliente ambiguo y confirma.
	res.Actions[2].PartyID = &cust.ID
	conf, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions)
	if err != nil || conf.Already || len(conf.Created) != 4 {
		t.Fatalf("confirmar: %v %+v", err, conf)
	}
	again, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions)
	if err != nil || !again.Already {
		t.Fatalf("confirmar dos veces: %v %+v", err, again)
	}
	db.Model(&models.Sale{}).Count(&sales)
	if sales != 3 {
		t.Fatalf("esperaba 3 ventas, hay %d", sales)
	}
	var voiceSales int64
	db.Model(&models.Sale{}).Where("source = ?", models.SourceVoice).Count(&voiceSales)
	if voiceSales != 3 {
		t.Fatalf("origen voz: %d", voiceSales)
	}
	var newParty models.Party
	if err := db.Where("name = ?", "Marta nueva clienta").First(&newParty).Error; err != nil {
		t.Fatal("el cliente nuevo debe crearse al confirmar")
	}

	// Productos del catálogo: sin decir precio ni total, basta el nombre.
	db.Create(&models.ServiceItem{EntityID: ent.ID, Name: "Jugo Hit", Price: ip(2000), Status: models.ServiceActive})
	fm.reply = `{"actions":[{"type":"sale","amount":0,"method":"cash","items":[{"name":"jugos hit","qty":2}]}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "vendí dos jugos hit")
	if len(res.Actions) != 1 || res.Actions[0].Amount != 4000 || len(res.Actions[0].Warnings) != 0 || !res.Actions[0].Lines[0].FromCatalog {
		t.Fatalf("venta desde el catálogo: %+v", res.Actions)
	}
	// Precio unitario dicho («tres mil cada uno») con total 6.000: respaldado por los renglones, sin advertencias.
	fm.reply = `{"actions":[{"type":"sale","amount_spoken":"3000 cada uno","amount":6000,"method":"cash","items":[{"name":"jugo hit","qty":2,"unit_price":3000}]}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "vendí dos jugos hit a 3000 cada uno")
	if res.Actions[0].Amount != 6000 || len(res.Actions[0].Warnings) != 0 {
		t.Fatalf("precio unitario dicho: %+v", res.Actions[0])
	}

	// Abono dictado a un cliente que no existe: advierte y no inventa.
	fm.reply = `{"actions":[{"type":"payment","amount_spoken":"5 mil","amount":5000,"party_name":"Zacarías"}]}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "Zacarías me abonó 5 mil")
	if len(res.Actions) != 1 || res.Actions[0].PartyID != nil || len(res.Actions[0].Warnings) == 0 {
		t.Fatalf("abono a desconocido: %+v", res.Actions)
	}
	if _, err := svc.ConfirmVoice(owner.ID, ent.ID, res.DraftID, res.Actions); !isInvalid(err) {
		t.Fatalf("confirmar abono sin cliente debe fallar: %v", err)
	}

	// Sin entender nada.
	fm.reply = `{"actions":[],"unclear":"¿Cuánto fue?"}`
	res, _ = svc.ParseVoice(owner.ID, ent.ID, "mmm")
	if len(res.Actions) != 0 || res.Message != "¿Cuánto fue?" {
		t.Fatalf("sin entender: %+v", res)
	}

	// Límite mensual.
	svc.VoiceMonthlyLimit = 1
	if _, err := svc.ParseVoice(owner.ID, ent.ID, "vendí algo"); err != ErrVoiceLimit {
		t.Fatalf("límite: %v", err)
	}
	// Otro usuario no ve nada.
	if _, err := svc.ParseVoice(uuid.New(), ent.ID, "x"); err != ErrLedgerForbidden {
		t.Fatalf("forbidden: %v", err)
	}
}
