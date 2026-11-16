package services

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"empre_backend/internal/ledger"
)

func invRow(name, kind, section, status string, isService bool, photo string) InventoryItem {
	return InventoryItem{Ref: uuid.New(), Kind: kind, Name: name, Section: section, Status: status, IsService: isService, Photo: photo}
}

func TestPageCursorRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 50, 4321} {
		got, err := DecodePageCursor(EncodePageCursor(n))
		if err != nil || got != n {
			t.Fatalf("cursor %d → %d, %v", n, got, err)
		}
	}
	if n, err := DecodePageCursor(""); err != nil || n != 0 {
		t.Fatalf("cursor vacío: %d, %v", n, err)
	}
	for _, bad := range []string{"xx", "eDox", EncodePageCursor(-1)} {
		if _, err := DecodePageCursor(bad); err == nil {
			t.Fatalf("cursor %q debería fallar", bad)
		}
	}
}

func TestParseInventoryPageQuery(t *testing.T) {
	q, err := ParseInventoryPageQuery("product", "  café ", "Bebidas", "low", "az", "", "500")
	if err != nil {
		t.Fatal(err)
	}
	if q.Q != "café" || q.Limit != InventoryPageMax || q.Offset != 0 {
		t.Fatalf("consulta: %+v", q)
	}
	if q, _ := ParseInventoryPageQuery("supply", "", "", "", "", "", ""); q.Limit != InventoryPageDefault {
		t.Fatalf("límite por defecto: %d", q.Limit)
	}
	for _, bad := range [][]string{{"todo", "", "", "", "", "", ""}, {"product", "", "", "raro", "", "", ""}, {"product", "", "", "", "precio", "", ""}, {"product", "", "", "", "", "", "0"}, {"product", "", "", "", "", "zz", ""}} {
		if _, err := ParseInventoryPageQuery(bad[0], bad[1], bad[2], bad[3], bad[4], bad[5], bad[6]); err == nil {
			t.Fatalf("debería fallar: %v", bad)
		}
	}
}

func TestPageInventoryTabsFiltersAndPages(t *testing.T) {
	var rows []InventoryItem
	for i := 0; i < 120; i++ {
		rows = append(rows, invRow(fmt.Sprintf("Producto %03d", i), "product", "Bebidas", ledger.StatusOK, false, "m1"))
	}
	burger := invRow("Hamburguesa clásica", "product", "Hamburguesas", ledger.StatusNone, false, "")
	burger.Variants = 2
	small := invRow("Hamburguesa clásica · Sencilla", "product", "Hamburguesas", ledger.StatusLow, false, "")
	small.ParentID, small.VariantLabel = &burger.Ref, "Sencilla"
	big := invRow("Hamburguesa clásica · Doble", "product", "Hamburguesas", ledger.StatusOK, false, "")
	big.ParentID, big.VariantLabel = &burger.Ref, "Doble"
	cafe := invRow("Café tinto", "product", "Bebidas", ledger.StatusOut, false, "m2")
	domicilio := invRow("Domicilio", "product", "Servicios", ledger.StatusNone, true, "")
	carne := invRow("Carne molida", "supply", "", ledger.StatusLow, false, "")
	carne.Group = "Carnes"
	rows = append(rows, burger, small, big, cafe, domicilio, carne)

	// Pestañas: los servicios y los insumos no se mezclan con los productos
	p := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, Limit: 50})
	if p.Total != 122 || len(p.Items) != 50 || p.NextCursor == "" {
		t.Fatalf("primera página: total %d, filas %d, cursor %q", p.Total, len(p.Items), p.NextCursor)
	}
	off, _ := DecodePageCursor(p.NextCursor)
	last := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, Limit: 50, Offset: 100})
	if off != 50 || len(last.Items) != 22 || last.NextCursor != "" {
		t.Fatalf("última página: %d filas, cursor %q", len(last.Items), last.NextCursor)
	}
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabService, Limit: 50}); s.Total != 1 || s.Items[0].Name != "Domicilio" {
		t.Fatalf("servicios: %+v", s.Items)
	}
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabSupply, Limit: 50, Section: "carnes"}); s.Total != 1 || s.Items[0].Name != "Carne molida" {
		t.Fatalf("insumos por sección: %+v", s.Items)
	}

	// Buscar sin tildes y por variante; las variantes vienen aparte, plegadas
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, Q: "cafe", Limit: 50}); s.Total != 1 || s.Items[0].Name != "Café tinto" {
		t.Fatalf("buscar sin tilde: %+v", s.Items)
	}
	s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, Q: "doble", Limit: 50})
	if s.Total != 1 || s.Items[0].Ref != burger.Ref || len(s.Variants) != 2 {
		t.Fatalf("buscar por variante: %d filas, %d variantes", s.Total, len(s.Variants))
	}

	// Estados: «se acaba» mira las variantes; «sin foto»; ordenar por existencias pone lo agotado primero
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, State: StateLow, Limit: 50}); s.Total != 1 || s.Items[0].Ref != burger.Ref {
		t.Fatalf("se acaban: %+v", s.Items)
	}
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, State: StateOut, Limit: 50}); s.Total != 1 || s.Items[0].Ref != cafe.Ref {
		t.Fatalf("agotados: %+v", s.Items)
	}
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, State: StateNoPhoto, Limit: 50}); s.Total != 1 || s.Items[0].Ref != burger.Ref {
		t.Fatalf("sin foto: %+v", s.Items)
	}
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, Sort: SortStock, Limit: 2}); s.Items[0].Ref != cafe.Ref || s.Items[1].Ref != burger.Ref {
		t.Fatalf("menos existencias: %s, %s", s.Items[0].Name, s.Items[1].Name)
	}
	// Un cursor más allá del final: página vacía, sin error
	if s := PageInventory(rows, InventoryPageQuery{Tab: TabProduct, Offset: 999, Limit: 50}); len(s.Items) != 0 || s.Total != 122 {
		t.Fatalf("fuera de rango: %+v", s)
	}
}

func TestCountInventory(t *testing.T) {
	burger := invRow("Hamburguesa", "product", "Hamburguesas", ledger.StatusNone, false, "")
	v := invRow("Hamburguesa · Doble", "product", "Hamburguesas", ledger.StatusOut, false, "")
	v.ParentID = &burger.Ref
	bowl := invRow("Bowl", "product", "Bowls", ledger.StatusLow, false, "m1")
	bowl2 := invRow("Bowl de res", "product", "Bowls", ledger.StatusOK, false, "m2")
	srv := invRow("Domicilio", "product", "", ledger.StatusNone, true, "")
	pan := invRow("Pan", "supply", "", ledger.StatusOK, false, "")
	pan.Group = "Panadería"
	alsoSupply := invRow("Gaseosa", "product", "Bebidas", ledger.StatusOK, false, "m3")
	alsoSupply.AlsoSupply = true
	c := CountInventory([]InventoryItem{burger, v, bowl, bowl2, srv, pan, alsoSupply})
	if c.Products.Total != 4 || c.Products.Out != 1 || c.Products.Low != 1 || c.Products.NoPhoto != 1 {
		t.Fatalf("productos: %+v", c.Products)
	}
	if len(c.Products.Sections) != 3 || c.Products.Sections[1].Name != "Bowls" || c.Products.Sections[1].Count != 2 {
		t.Fatalf("secciones: %+v", c.Products.Sections)
	}
	if c.Services.Total != 1 || c.Supplies.Total != 2 {
		t.Fatalf("servicios %d, insumos %d", c.Services.Total, c.Supplies.Total)
	}
}
