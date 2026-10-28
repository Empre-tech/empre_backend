package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// El análisis de un rango libre: totales contra el periodo anterior, serie por día, cuándo se vende, ganancia por
// producto e inventario en plata.
func TestAnalytics(t *testing.T) {
	db, svc, owner, other, ent := ledgerFixture(t)
	now := nowCO()
	day := func(back, hour int) *time.Time {
		d := time.Date(now.Year(), now.Month(), now.Day(), hour, 30, 0, 0, now.Location()).AddDate(0, 0, -back)
		return &d
	}
	f := func(back int) string { return now.AddDate(0, 0, -back).Format("2006-01-02") }

	aceite := stockProduct(t, db, ent, "Aceite", 10_000)
	if err := svc.SetItemCost(owner.ID, ent.ID, aceite.ID, ip(7_000)); err != nil {
		t.Fatal(err)
	}
	sale := func(at *time.Time, method string, lines []LineInput, total *int) {
		t.Helper()
		if _, _, err := svc.CreateSale(owner.ID, ent.ID, SaleInput{ClientID: uuid.New(), OccurredAt: at, Method: method, Lines: lines, Total: total}); err != nil {
			t.Fatal(err)
		}
	}
	// Rango: hace 2 días y ayer. Antes: hace 4 y 3 días.
	sale(day(2, 12), "cash", []LineInput{{ItemID: &aceite.ID, Qty: 2}}, nil)            // 20.000, mediodía
	sale(day(1, 9), "transfer", []LineInput{{Name: "Bolsa", Qty: 1, UnitPrice: ip(5_000)}}, nil) // 5.000 sin costo, mañana
	sale(day(4, 20), "cash", nil, ip(10_000))                                             // periodo anterior
	if _, _, err := svc.CreateExpense(owner.ID, ent.ID, ExpenseInput{ClientID: uuid.New(), OccurredAt: day(1, 10), Amount: 4_000, Category: "servicios", Method: "cash"}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Analytics(other.ID, ent.ID, f(2), f(1), ""); err == nil {
		t.Fatal("otro usuario no puede ver el análisis")
	}
	if _, err := svc.Analytics(owner.ID, ent.ID, f(1), f(2), ""); !isInvalid(err) {
		t.Fatal("fechas al revés deben fallar")
	}
	if _, err := svc.Analytics(owner.ID, ent.ID, now.AddDate(-2, 0, 0).Format("2006-01-02"), f(0), ""); !isInvalid(err) {
		t.Fatal("más de un año debe fallar")
	}

	a, err := svc.Analytics(owner.ID, ent.ID, f(2), f(1), "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Days != 2 || a.Compare != "prev" || a.CompareFrom != f(4) || a.CompareTo != f(3) {
		t.Fatalf("rango y comparación: %+v", a)
	}
	c := a.Current
	if c.SalesTotal != 25_000 || c.SalesCount != 2 || c.ExpensesTotal != 4_000 || c.Profit != 21_000 || c.AvgTicket != 12_500 {
		t.Fatalf("totales: %+v", c)
	}
	if c.ByMethod.Cash != 20_000 || c.ByMethod.Transfer != 5_000 || c.ExpensesByCat["servicios"] != 4_000 {
		t.Fatalf("métodos y categorías: %+v", c)
	}
	if c.GrossMargin != 6_000 || c.MarginCoverage != 80 || c.CostedRevenue != 20_000 {
		t.Fatalf("margen: %d cobertura %d", c.GrossMargin, c.MarginCoverage)
	}
	if a.Previous.SalesTotal != 10_000 {
		t.Fatalf("periodo anterior: %+v", a.Previous)
	}
	if a.Bucket != "day" || len(a.Series) != 2 || a.Series[0].Sales != 20_000 || a.Series[1].Sales != 5_000 || a.Series[1].Expenses != 4_000 {
		t.Fatalf("serie: %+v", a.Series)
	}
	wd := func(back int) int { return (int(now.AddDate(0, 0, -back).Weekday()) + 6) % 7 }
	if a.Heat[wd(2)][1] != 20_000 || a.Heat[wd(1)][0] != 5_000 {
		t.Fatalf("cuándo se vende: %+v", a.Heat)
	}
	if len(a.Products) != 2 || a.Products[0].Name != "Aceite" || a.Products[0].Profit == nil || *a.Products[0].Profit != 6_000 || *a.Products[0].MarginPct != 30 {
		t.Fatalf("productos: %+v", a.Products)
	}
	if a.Products[1].Profit != nil || a.UncostedCount != 1 || a.UncostedRevenue != 5_000 {
		t.Fatalf("sin costo: %+v %d %d", a.Products[1], a.UncostedCount, a.UncostedRevenue)
	}

	// Mismas fechas del año anterior: no hay ventas.
	y, err := svc.Analytics(owner.ID, ent.ID, f(2), f(1), "year")
	if err != nil || y.Compare != "year" || y.Previous.SalesTotal != 0 {
		t.Fatalf("comparar con el año anterior: %v %+v", err, y.Previous)
	}

	// Inventario en plata: 10 aceites a 7.000; se dañan 2 hoy; el reloj no se vende hace rato.
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: aceite.ID, Counted: fp(10), Reason: "conteo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: aceite.ID, Delta: fp(-2), Reason: "danado"}); err != nil {
		t.Fatal(err)
	}
	reloj := stockProduct(t, db, ent, "Reloj", 100_000)
	if err := svc.SetItemCost(owner.ID, ent.ID, reloj.ID, ip(60_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: reloj.ID, Counted: fp(3), Reason: "conteo"}); err != nil {
		t.Fatal(err)
	}
	today, err := svc.Analytics(owner.ID, ent.ID, f(0), f(0), "")
	if err != nil {
		t.Fatal(err)
	}
	inv := today.Inventory
	if inv.ValueCost != 8*7_000+3*60_000 || inv.ValuePrice != 8*10_000+3*100_000 || inv.Valued != 2 {
		t.Fatalf("valor: %+v", inv)
	}
	if inv.LostTotal != 14_000 || len(inv.Lost) != 1 || inv.Lost[0].Name != "Aceite" {
		t.Fatalf("pérdidas: %+v", inv.Lost)
	}
	if inv.IdleTotal != 180_000 || len(inv.Idle) != 1 || inv.Idle[0].Name != "Reloj" || inv.Idle[0].Text != "Sin ventas aún" {
		t.Fatalf("plata quieta: %+v", inv.Idle)
	}
	// La serie larga se agrupa por semana.
	w, err := svc.Analytics(owner.ID, ent.ID, now.AddDate(0, 0, -60).Format("2006-01-02"), f(0), "")
	if err != nil || w.Bucket != "week" || len(w.Series) < 9 {
		t.Fatalf("serie semanal: %v %s %d", err, w.Bucket, len(w.Series))
	}
}
