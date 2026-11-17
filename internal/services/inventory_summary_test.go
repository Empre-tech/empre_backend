package services

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
)

func TestCountAlertsFollowsTheAppRules(t *testing.T) {
	serv := 3.0
	pan := invRow("Pan", "supply", "", ledger.StatusOut, false, "")
	pan.Mode = "exact"
	cafe := invRow("Café", "product", "Bebidas", ledger.StatusLow, false, "")
	cafe.Mode = "light"
	bowl := invRow("Bowl", "product", "Bowls", ledger.StatusOK, false, "")
	bowl.Mode = "exact"
	bowl.Servings, bowl.Sold28 = &serv, 28*5 // alcanza para 3, se venden 5 al día: en riesgo
	burger := invRow("Hamburguesa", "product", "", ledger.StatusOK, false, "")
	burger.Mode = "exact"
	burger.Ingredients = []NameStatus{{Name: "Pan", Status: ledger.StatusOut}}
	group := invRow("Gaseosa", "product", "", ledger.StatusOut, false, "")
	group.Mode, group.Variants = "exact", 2 // un producto con variantes no cuenta por sí mismo
	corte := invRow("Corte", "product", "", ledger.StatusNone, true, "")
	sinControl := invRow("Arroz", "product", "", ledger.StatusOut, false, "")
	sinControl.Mode = "unset" // sin control no avisa

	c := CountAlerts([]InventoryItem{pan, cafe, bowl, burger, group, corte, sinControl})
	if c.Out != 1 || c.Soon != 1 || c.Risk != 2 || c.Total != 4 {
		t.Fatalf("avisos: %+v", c)
	}
}

func TestSummarizeInventory(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	old, recent := now.AddDate(0, 0, -(CatalogFreshDays + 5)), now.AddDate(0, 0, -3) // uno vencido y uno al día
	p := func(n int) *int { return &n }
	items := []models.ServiceItem{
		{ID: uuid.New(), Name: "Arroz", Price: p(3500), PriceUpdatedAt: &recent, KindChecked: true},
		{ID: uuid.New(), Name: "Aceite", Price: p(11500), PriceUpdatedAt: &old, KindChecked: false},
		{ID: uuid.New(), Name: "Domicilio", Price: nil, KindChecked: false, Kind: models.ItemService},
	}
	arroz := invRow("Arroz", "product", "", ledger.StatusOK, false, "")
	arroz.Mode = "exact"
	aceite := invRow("Aceite", "product", "", ledger.StatusNone, false, "")
	aceite.Mode = "unset"
	ov := InventoryOverview{Kind: "shop", Category: "Tiendas", Unset: 1, Items: []InventoryItem{arroz, aceite}}

	s := SummarizeInventory(ov, items, now)
	if s.Tracked != 1 || s.Unset != 1 || s.Kind != "shop" {
		t.Fatalf("control: %+v", s)
	}
	if s.UncheckedTotal != 2 || len(s.Unchecked) != 2 || s.Unchecked[1].Kind != models.ItemService {
		t.Fatalf("por revisar: %+v", s.Unchecked)
	}
	if s.Prices.Priced != 2 || s.Prices.Stale != 1 || !s.Prices.OldestAt.Equal(old) {
		t.Fatalf("precios: %+v", s.Prices)
	}
}

func TestPickInventoryByRefOrItem(t *testing.T) {
	a := invRow("A", "product", "", ledger.StatusOK, false, "")
	itemID := uuid.New()
	a.ItemID = &itemID
	b := invRow("B", "supply", "", ledger.StatusOK, false, "")
	c := invRow("C", "product", "", ledger.StatusOK, false, "")
	got := PickInventory([]InventoryItem{a, b, c}, []uuid.UUID{itemID, b.Ref, uuid.New()})
	if len(got) != 2 || got[0].Name != "A" || got[1].Name != "B" {
		t.Fatalf("filas: %+v", got)
	}
	if n := InventoryNames([]InventoryItem{a, b}); len(n) != 2 || n[1].Kind != "supply" {
		t.Fatalf("nombres: %+v", n)
	}
}
