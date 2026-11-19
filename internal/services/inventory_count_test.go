package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func f(v float64) *float64 { return &v }

func TestCountDeltaKeepsSalesMadeWhileCounting(t *testing.T) {
	// Al empezar había 10; mientras contaba se vendieron 2 (ahora hay 8); contó 9: falta 1, queda en 7.
	if d := countDelta(8, 9, f(10), true); d != -1 {
		t.Fatalf("delta con ventas en medio: %v", d)
	}
	// Cuadra con lo que había al empezar: no cambia nada aunque se haya vendido.
	if d := countDelta(8, 10, f(10), true); d != 0 {
		t.Fatalf("cuadra: %v", d)
	}
	// Nunca se había contado: lo contado es lo que hay.
	if d := countDelta(0, 12, f(0), false); d != 12 {
		t.Fatalf("primer conteo: %v", d)
	}
	// Nunca deja la existencia bajo cero.
	if d := countDelta(1, 0, f(5), true); d != -1 {
		t.Fatalf("bajo cero: %v", d)
	}
	// Sin lo que había al empezar, fija la cantidad.
	if d := countDelta(4, 6, nil, true); d != 2 {
		t.Fatalf("sin esperado: %v", d)
	}
}

func TestValidCountInput(t *testing.T) {
	ok := CountInput{ClientID: uuid.New(), Tab: TabProduct, Lines: []CountLine{{Ref: uuid.New(), Counted: f(3), Expected: f(4), Reason: "danado"}, {Ref: uuid.New(), Level: "low"}}}
	if err := validCountInput(ok); err != nil {
		t.Fatal(err)
	}
	bad := []CountInput{
		{Tab: TabProduct, Lines: ok.Lines},                                                             // sin client_id
		{ClientID: uuid.New(), Tab: TabService, Lines: ok.Lines},                                       // los servicios no se cuentan
		{ClientID: uuid.New(), Tab: TabProduct},                                                        // sin filas
		{ClientID: uuid.New(), Tab: TabProduct, Lines: []CountLine{{Ref: uuid.New()}}},                 // ni cantidad ni nivel
		{ClientID: uuid.New(), Tab: TabProduct, Lines: []CountLine{{Ref: uuid.New(), Counted: f(-1)}}}, // negativa
		{ClientID: uuid.New(), Tab: TabProduct, Lines: []CountLine{{Ref: uuid.New(), Counted: f(1), Reason: "regalo"}}},
	}
	for i, in := range bad {
		if err := validCountInput(in); err == nil {
			t.Fatalf("debería fallar #%d", i)
		}
	}
}

func TestAttachCountDates(t *testing.T) {
	at := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	c := InventoryCounts{Products: TabCounts{Sections: []SectionCount{{Name: "Lácteos y huevos"}, {Name: "Bebidas"}}}, Supplies: TabCounts{Sections: []SectionCount{{Name: "Bebidas"}}}}
	AttachCountDates(&c, map[string]time.Time{countKey(TabProduct, "lacteos y huevos"): at})
	if c.Products.Sections[0].CountedAt == nil || !c.Products.Sections[0].CountedAt.Equal(at) {
		t.Fatalf("lácteos: %+v", c.Products.Sections[0])
	}
	if c.Products.Sections[1].CountedAt != nil || c.Supplies.Sections[0].CountedAt != nil {
		t.Fatal("una sección sin conteo, o de otra pestaña, no lleva fecha")
	}
}
