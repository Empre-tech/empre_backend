package ledger

import "testing"

func TestProfileForCategory(t *testing.T) {
	cases := map[string]Profile{
		"Tienda de barrio":       {KindShop, ModeLight, false},
		"Panadería y repostería": {KindShop, ModeLight, false},
		"Perfumería":             {KindValuable, ModeExact, false},
		"Celulares y accesorios": {KindValuable, ModeExact, false},
		"Restaurante":            {KindFood, ModeNone, true},
		"Comidas rápidas":        {KindFood, ModeNone, true},
		"Peluquería y barbería":  {KindService, ModeExact, false},
		"Algo muy raro":          {KindOther, ModeLight, false},
		"":                       {KindOther, ModeLight, false},
	}
	for in, want := range cases {
		if got := ProfileForCategory(in); got != want {
			t.Errorf("%q: got %+v want %+v", in, got, want)
		}
	}
}

func ip(n int) *int         { return &n }
func fp(n float64) *float64 { return &n }

func TestRecommendMode(t *testing.T) {
	shop := Profile{KindShop, ModeLight, false}
	hair := Profile{KindService, ModeExact, false}
	food := Profile{KindFood, ModeNone, true}
	cases := []struct {
		name string
		p    Profile
		f    ItemFacts
		want string
	}{
		{"servicio nunca lleva inventario", hair, ItemFacts{IsService: true, Price: ip(80000)}, ModeNone},
		{"producto caro siempre exacto, aun en tienda", shop, ItemFacts{Price: ip(120000)}, ModeExact},
		{"plato barato: no lleva existencias", food, ItemFacts{Price: ip(18000)}, ModeNone},
		{"bebida cara de un restaurante: exacto", food, ItemFacts{Price: ip(60000)}, ModeExact},
		{"peluquería con producto de 5 unidades: exacto", hair, ItemFacts{Price: ip(25000), Units: fp(5)}, ModeExact},
		{"tienda con 40 unidades: semáforo", shop, ItemFacts{Price: ip(3000), Units: fp(40)}, ModeLight},
		{"tienda con pocas unidades: exacto", shop, ItemFacts{Price: ip(3000), Units: fp(4)}, ModeExact},
		{"sin datos: manda el tipo de negocio", shop, ItemFacts{Price: ip(3000)}, ModeLight},
		{"sin precio: manda el tipo de negocio", hair, ItemFacts{}, ModeExact},
	}
	for _, c := range cases {
		if got := RecommendMode(c.p, c.f); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestDaysLeftAndStatus(t *testing.T) {
	if _, ok := DaysLeft(10, 0); ok {
		t.Fatal("sin ventas no hay estimación")
	}
	d, ok := DaysLeft(6, 28) // vende 1 al día
	if !ok || d != 6 {
		t.Fatalf("días: %v %v", d, ok)
	}
	// Exacto sin contar: no se avisa nada.
	if s := Status(ModeExact, false, 0, "", nil, 0); s != StatusUnknown {
		t.Fatalf("sin contar: %s", s)
	}
	if s := Status(ModeExact, true, 0, "", nil, 10); s != StatusOut {
		t.Fatalf("agotado: %s", s)
	}
	if s := Status(ModeExact, true, 2, "", nil, 28); s != StatusLow { // 2 días de venta
		t.Fatalf("poco por días: %s", s)
	}
	if s := Status(ModeExact, true, 50, "", nil, 28); s != StatusOK {
		t.Fatalf("bien: %s", s)
	}
	if s := Status(ModeExact, true, 3, "", fp(5), 0); s != StatusLow {
		t.Fatalf("poco por umbral: %s", s)
	}
	if Status(ModeLight, false, 0, LevelOut, nil, 0) != StatusOut || Status(ModeLight, false, 0, LevelLow, nil, 0) != StatusLow || Status(ModeLight, false, 0, "", nil, 0) != StatusOK {
		t.Fatal("semáforo")
	}
	if Status(ModeNone, true, 0, "", nil, 0) != StatusNone {
		t.Fatal("sin control")
	}
	if !SoldOut(ModeLight, false, 0, LevelOut) || SoldOut(ModeExact, false, 0, "") || !SoldOut(ModeExact, true, 0, "") || SoldOut(ModeNone, true, 0, "") {
		t.Fatal("agotado público")
	}
}

func TestKindForNameAndProfile(t *testing.T) {
	cases := map[string]string{"Restaurantes": KindFood, "Peluquería": KindService, "Tiendas y comercio": KindShop, "Joyería": KindValuable, "Cosas raras": ""}
	for name, want := range cases {
		if got := KindForName(name); got != want {
			t.Errorf("%s: %q, esperaba %q", name, got, want)
		}
	}
	if ProfileForKind("food").Mode != ModeNone || !ProfileForKind("food").QuickSale {
		t.Error("comida: platos sin inventario y atención rápida")
	}
	if ProfileForKind("nada").Kind != KindOther || ValidKind("nada") || !ValidKind("valuable") {
		t.Error("un tipo desconocido es «other»")
	}
}
