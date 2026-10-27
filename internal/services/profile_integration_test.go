package services

import (
	"testing"

	"empre_backend/internal/database"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
)

// El tipo de negocio se guarda (no se adivina): la migración lo llena, el dueño lo cambia y renombrar la categoría no
// cambia nada.
func TestBusinessProfile(t *testing.T) {
	db, svc, owner, _, ent := ledgerFixture(t) // categoría «Comida»
	rare := models.Category{Name: "Cosas raras"}
	if err := db.Create(&rare).Error; err != nil {
		t.Fatal(err)
	}
	child := models.Category{Name: "Corrientazo", ParentID: &ent.CategoryID}
	if err := db.Create(&child).Error; err != nil {
		t.Fatal(err)
	}

	unknown, err := database.BackfillProfiles(db)
	if err != nil {
		t.Fatal(err)
	}
	var c models.Category
	db.First(&c, "id = ?", ent.CategoryID)
	if c.Profile != "food" {
		t.Fatalf("«Comida» debe quedar como food: %q", c.Profile)
	}
	db.First(&c, "id = ?", child.ID)
	if c.Profile != "food" {
		t.Fatalf("la subcategoría hereda del padre: %q", c.Profile)
	}
	found := false
	for _, n := range unknown {
		if n == "Cosas raras" {
			found = true
		}
	}
	if !found {
		t.Fatalf("«Cosas raras» debe quedar en la lista para revisar: %v", unknown)
	}
	var e models.Entity
	db.First(&e, "id = ?", ent.ID)
	if e.BusinessProfile != "food" {
		t.Fatalf("el negocio toma el tipo de su categoría: %q", e.BusinessProfile)
	}
	// Idempotente: correrla de nuevo no cambia nada.
	if _, err := database.BackfillProfiles(db); err != nil {
		t.Fatal(err)
	}

	// Renombrar la categoría no cambia el tipo.
	db.Model(&models.Category{}).Where("id = ?", ent.CategoryID).Update("name", "Sabores de la casa")
	ov, err := svc.Inventory(owner.ID, ent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ov.Kind != "food" {
		t.Fatalf("renombrar la categoría no debe cambiar el tipo: %s", ov.Kind)
	}

	// El dueño elige otro tipo: manda el suyo.
	db.Model(&models.Entity{}).Where("id = ?", ent.ID).Update("business_profile", "shop")
	ov, _ = svc.Inventory(owner.ID, ent.ID)
	if ov.Kind != "shop" {
		t.Fatalf("manda el tipo que eligió el dueño: %s", ov.Kind)
	}

	// Cambiar de categoría: si el tipo era el sugerido, sigue a la nueva; si el dueño lo eligió, se respeta.
	es := &EntityService{Repo: repository.NewEntityRepository(db)}
	shopCat := models.Category{Name: "Tienda de barrio", Profile: "shop"}
	db.Create(&shopCat)
	moved := models.Entity{CategoryID: ent.CategoryID, BusinessProfile: "food"}
	moved.CategoryID = shopCat.ID
	es.FollowCategoryProfile(&moved, ent.CategoryID)
	if moved.BusinessProfile != "shop" {
		t.Fatalf("sin elección propia, sigue a la nueva categoría: %s", moved.BusinessProfile)
	}
	chosen := models.Entity{CategoryID: shopCat.ID, BusinessProfile: "service"}
	es.FollowCategoryProfile(&chosen, ent.CategoryID)
	if chosen.BusinessProfile != "service" {
		t.Fatalf("lo que eligió el dueño se respeta: %s", chosen.BusinessProfile)
	}
}
