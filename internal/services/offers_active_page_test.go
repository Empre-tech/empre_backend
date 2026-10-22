package services

import (
	"fmt"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Las ofertas vigentes de un negocio salen con tope, las que terminan antes primero, a igualdad la de más
// cupos libres; las vencidas y las agotadas no cuentan y `total` dice cuántas hay en realidad.
func TestActiveOffers_LimitOrderAndTotal(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cat := models.Category{Name: "Cafeterías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(name string) uuid.UUID {
		e := models.Entity{OwnerID: owner, Name: name, CategoryID: cat.ID, Latitude: 11.2, Longitude: -74.2}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	shop, other := mk("Café"), mk("Otro")
	now := time.Now()
	add := func(entity uuid.UUID, title string, ends time.Duration, total, used int) {
		o := models.Offer{EntityID: entity, Type: models.OfferHours, Title: title, Price: "1", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(ends), TotalSlots: total, UsedSlots: used, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
		if err := db.Omit(clause.Associations).Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}
	add(shop, "D: termina en 3 h", 3*time.Hour, 10, 0)
	add(shop, "B: igual hora, menos cupos", 2*time.Hour, 10, 8)
	add(shop, "A: igual hora, más cupos", 2*time.Hour, 10, 1)
	add(shop, "C: termina en 2 h 30", 150*time.Minute, 10, 0)
	add(shop, "vencida", -time.Minute, 10, 0)
	add(shop, "agotada", time.Hour, 5, 5)
	for i := 0; i < 6; i++ {
		add(shop, fmt.Sprintf("E%d", i), time.Duration(10+i)*time.Hour, 10, 0)
	}
	add(other, "de otro negocio", time.Hour, 10, 0)

	repo := repository.NewOfferRepository(db)
	list, total, err := repo.FindActiveByEntity(shop, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 10 || len(list) != 10 {
		t.Fatalf("sin tope: %d de %d (se esperaban 10: sin vencidas, agotadas ni de otro)", len(list), total)
	}
	// A y B terminan a la misma hora; los segundos de diferencia de creación no mandan, mandan los cupos.
	var order []string
	for _, o := range list[:4] {
		order = append(order, o.Title[:1])
	}
	if fmt.Sprint(order) != "[A B C D]" {
		t.Fatalf("orden: %v", order)
	}

	top, total, err := repo.FindActiveByEntity(shop, now, 6)
	if err != nil || len(top) != 6 || total != 10 {
		t.Fatalf("con tope 6: %d de %d (%v)", len(top), total, err)
	}
	if top[0].Title[:1] != "A" {
		t.Fatalf("la primera de la lista cortada debe ser la que termina antes con más cupos: %s", top[0].Title)
	}
	if capped, _, _ := repo.FindActiveByEntity(shop, now, 9999); len(capped) != 10 {
		t.Fatalf("un tope enorme se recorta, no falla: %d", len(capped))
	}
	if none, n, err := repo.FindActiveByEntity(uuid.New(), now, 5); err != nil || len(none) != 0 || n != 0 {
		t.Fatalf("negocio sin ofertas: %v %d %v", none, n, err)
	}
}
