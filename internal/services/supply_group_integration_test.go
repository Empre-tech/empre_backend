package services

import (
	"testing"

	"github.com/google/uuid"
)

// Los insumos se agrupan (Carnes, Verduras…) para encontrarlos entre cientos.
func TestSupplyGroups(t *testing.T) {
	_, svc, owner, _, ent := ledgerFixture(t)

	carne, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Carne molida", Group: "  carnes "})
	if err != nil {
		t.Fatal(err)
	}
	if carne.Group != "Carnes" {
		t.Fatalf("el grupo debe quedar limpio y con mayúscula: %q", carne.Group)
	}
	sal, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "Sal"})
	if err != nil || sal.Group != "" {
		t.Fatalf("sin grupo: %v %q", err, sal.Group)
	}
	// Si ya existía sin grupo y lo crean otra vez con grupo, se lo pone (y no se duplica).
	again, err := svc.CreateSupply(owner.ID, ent.ID, SupplyInput{ClientID: uuid.New(), Name: "sal", Group: "Especias"})
	if err != nil || again.ID != sal.ID || again.Group != "Especias" {
		t.Fatalf("reusar y agrupar: %v %v %q", err, again.ID == sal.ID, again.Group)
	}

	// Cambiar y quitar el grupo desde los ajustes.
	g := "Verduras"
	ov, err := svc.UpdateStockSettings(owner.ID, ent.ID, carne.ID, ItemSettings{Group: &g})
	if err != nil {
		t.Fatal(err)
	}
	groupOf := func(ov InventoryOverview, id uuid.UUID) string {
		for _, it := range ov.Items {
			if it.Ref == id {
				return it.Group
			}
		}
		t.Fatalf("no está %s", id)
		return ""
	}
	if got := groupOf(ov, carne.ID); got != "Verduras" {
		t.Fatalf("grupo nuevo: %q", got)
	}
	empty := ""
	ov, err = svc.UpdateStockSettings(owner.ID, ent.ID, carne.ID, ItemSettings{Group: &empty})
	if err != nil || groupOf(ov, carne.ID) != "" {
		t.Fatalf("quitar grupo: %v", err)
	}
	if groupOf(ov, sal.ID) != "Especias" {
		t.Fatal("el otro insumo conserva su grupo")
	}
}
