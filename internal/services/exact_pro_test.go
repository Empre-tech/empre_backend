package services

import (
	"errors"
	"testing"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// TestExactCountIsPro: en Base no se lleva la cantidad exacta (2026-11-22). Un insumo nuevo en exacta, pasar uno a
// exacta, fijar lo contado o contar una sección responden ErrProRequired; el semáforo sigue funcionando.
func TestExactCountIsPro(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t)
	if err := db.Where("entity_id = ?", ent.ID).Delete(&models.Subscription{}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Harina", Mode: ledger.ModeExact}); !errors.Is(err, ErrProRequired) {
		t.Fatalf("insumo en exacta: esperaba ErrProRequired, llegó %v", err)
	}
	sup, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Azúcar"})
	if err != nil {
		t.Fatal(err)
	}
	if sup.Mode != ledger.ModeLight {
		t.Fatalf("en Base el insumo va en semáforo, quedó %q", sup.Mode)
	}
	exact := ledger.ModeExact
	if _, err := svc.UpdateStockSettings(owner.ID, ent.ID, sup.ID, ItemSettings{Mode: &exact}); !errors.Is(err, ErrProRequired) {
		t.Fatalf("pasar a exacta: esperaba ErrProRequired, llegó %v", err)
	}
	n := 12.0
	if _, err := svc.Adjust(owner.ID, ent.ID, AdjustInput{ClientID: uuid.New(), Ref: sup.ID, Counted: &n}); !errors.Is(err, ErrProRequired) {
		t.Fatalf("fijar lo contado: esperaba ErrProRequired, llegó %v", err)
	}
	if _, err := svc.CountSection(owner.ID, ent.ID, CountInput{ClientID: uuid.New(), Tab: "supply", Section: "Otros"}); !errors.Is(err, ErrProRequired) {
		t.Fatalf("contar una sección: esperaba ErrProRequired, llegó %v", err)
	}
	var got models.StockItem
	if err := db.First(&got, "id = ?", sup.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Mode != ledger.ModeLight {
		t.Fatalf("el insumo debía seguir en semáforo, quedó %q", got.Mode)
	}
	if err := svc.SetLevel(owner.ID, ent.ID, sup.ID, ledger.LevelLow, uuid.New()); err != nil {
		t.Fatalf("el semáforo es de Base: %v", err)
	}

	// Con los planes apagados (desarrollo) la cantidad exacta vuelve.
	svc.PlansEnforced = false
	if _, err := svc.UpdateStockSettings(owner.ID, ent.ID, sup.ID, ItemSettings{Mode: &exact}); err != nil {
		t.Fatalf("sin planes: %v", err)
	}
}
