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

// La lista de reseñas se filtra por nota, verificadas y con comentario; el total es el de las que cumplen el
// filtro, las más recientes salen primero y la página se corta en el tope.
func TestReviews_Filters(t *testing.T) {
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
	n := 0
	add := func(entity uuid.UUID, rating int, comment string, verified bool) {
		n++
		at := now.Add(-time.Duration(n) * time.Hour)
		r := models.Review{EntityID: entity, UserID: newUser(t, db), Rating: rating, Comment: comment, CreatedAt: at, UpdatedAt: at}
		if verified {
			c := uuid.New()
			r.ClaimID = &c
		}
		if err := db.Omit(clause.Associations).Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	add(shop, 5, "Excelente", true)         // 1
	add(shop, 5, "", true)                  // 2: verificada sin comentario
	add(shop, 4, "   ", false)              // 3: solo espacios = sin comentario
	add(shop, 1, "Muy mal servicio", false) // 4
	add(shop, 5, "Muy bueno", false)        // 5
	add(shop, 1, "Tardaron", true)          // 6
	add(other, 5, "De otro negocio", true)

	svc := NewReviewService(repository.NewReviewRepository(db), nil)
	cases := []struct {
		name string
		f    repository.ReviewFilter
		want int
	}{
		{"sin filtro", repository.ReviewFilter{}, 6},
		{"solo 5 estrellas", repository.ReviewFilter{Stars: 5}, 3},
		{"solo 1 estrella", repository.ReviewFilter{Stars: 1}, 2},
		{"solo verificadas", repository.ReviewFilter{Verified: true}, 3},
		{"solo con comentario (los espacios no cuentan)", repository.ReviewFilter{WithComment: true}, 4},
		{"verificadas con comentario", repository.ReviewFilter{Verified: true, WithComment: true}, 2},
		{"1 estrella verificada con comentario", repository.ReviewFilter{Stars: 1, Verified: true, WithComment: true}, 1},
		{"nota fuera de rango se ignora", repository.ReviewFilter{Stars: 9}, 6},
		{"sin coincidencias", repository.ReviewFilter{Stars: 3}, 0},
	}
	for _, c := range cases {
		list, total, err := svc.FindByEntityFiltered(shop, 1, 20, c.f)
		if err != nil || len(list) != c.want || int(total) != c.want {
			t.Errorf("%s: %d reseñas, total %d (%v), se esperaban %d", c.name, len(list), total, err, c.want)
		}
	}

	// Orden: las más recientes primero, también con filtro.
	list, _, _ := svc.FindByEntityFiltered(shop, 1, 20, repository.ReviewFilter{Verified: true})
	if len(list) != 3 || list[0].Comment != "Excelente" || list[2].Comment != "Tardaron" {
		t.Fatalf("orden de verificadas: %+v", list)
	}

	// Paginación: el total no cambia con la página y la última página es corta.
	p1, total, _ := svc.FindByEntityFiltered(shop, 1, 4, repository.ReviewFilter{})
	p2, total2, _ := svc.FindByEntityFiltered(shop, 2, 4, repository.ReviewFilter{})
	if len(p1) != 4 || len(p2) != 2 || total != 6 || total2 != 6 {
		t.Fatalf("paginación: %d y %d de %d/%d", len(p1), len(p2), total, total2)
	}

	// Tope de página: pedir más de lo permitido se recorta.
	for i := 0; i < repository.MaxReviewPage+5; i++ {
		add(other, 4, fmt.Sprintf("r%d", i), false)
	}
	big, _, _ := svc.FindByEntityFiltered(other, 1, 500, repository.ReviewFilter{})
	if len(big) != repository.MaxReviewPage {
		t.Fatalf("tope de página: %d", len(big))
	}
}
