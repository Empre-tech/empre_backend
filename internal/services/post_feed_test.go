package services

import (
	"fmt"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Las publicaciones de un negocio salen por páginas, las más recientes primero, con sus archivos en orden, y se
// pueden pedir solo las que traen video o solo las que traen foto.
func TestPostFeed_PagesAndKinds(t *testing.T) {
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
	// post crea una publicación con archivos (tipos de contenido en orden); devuelve su id.
	post := func(entity uuid.UUID, caption string, types ...string) uuid.UUID {
		n++
		p := models.Post{EntityID: entity, Caption: caption, CreatedAt: now.Add(-time.Duration(n) * time.Hour), UpdatedAt: now}
		if err := db.Omit(clause.Associations).Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		for i, ct := range types {
			m := models.Media{S3Key: fmt.Sprintf("k/%s", uuid.NewString()), ContentType: ct}
			if err := db.Create(&m).Error; err != nil {
				t.Fatal(err)
			}
			ph := models.EntityPhoto{PostID: p.ID, EntityID: entity, MediaID: m.ID, Order: len(types) - i} // el orden manda, no la inserción
			if err := db.Omit(clause.Associations).Create(&ph).Error; err != nil {
				t.Fatal(err)
			}
		}
		return p.ID
	}
	post(shop, "1: foto", "image/jpeg")
	post(shop, "2: video", "video/mp4")
	post(shop, "3: varias fotos", "image/jpeg", "image/png", "image/jpeg")
	post(shop, "4: mixta", "image/jpeg", "video/mp4")
	post(shop, "5: sin archivos")
	for i := 0; i < 30; i++ {
		post(shop, fmt.Sprintf("extra %d", i), "image/jpeg")
	}
	post(other, "de otro negocio", "image/jpeg")

	svc := NewPostFeedService(db, nil)
	all, total, err := svc.Page(shop, 1, 100, PostKindAll)
	if err != nil {
		t.Fatal(err)
	}
	// 34 publicaciones con archivo (la vacía y la de otro negocio no cuentan); con el tope de 50 caben todas.
	if total != 34 || len(all) != 34 {
		t.Fatalf("todas: %d de %d", len(all), total)
	}
	if all[0].Caption != "1: foto" || all[1].Caption != "2: video" {
		t.Fatalf("orden (más reciente primero): %q, %q", all[0].Caption, all[1].Caption)
	}
	if len(all[2].Photos) != 3 || all[2].Photos[0].Order > all[2].Photos[1].Order {
		t.Fatalf("archivos en orden: %+v", all[2].Photos)
	}
	if all[2].Photos[0].Media.ContentType == "" {
		t.Fatal("cada archivo trae su tipo de contenido")
	}

	// Páginas: 30 y luego el resto; el total no cambia.
	p1, t1, _ := svc.Page(shop, 1, 0, PostKindAll)
	p2, t2, _ := svc.Page(shop, 2, 0, PostKindAll)
	if len(p1) != 30 || len(p2) != 4 || t1 != 34 || t2 != 34 {
		t.Fatalf("paginación: %d y %d de %d/%d", len(p1), len(p2), t1, t2)
	}
	if p2[0].Caption == p1[29].Caption {
		t.Fatal("las páginas no se repiten")
	}

	videos, vt, _ := svc.Page(shop, 1, 30, PostKindVideo)
	if vt != 2 || len(videos) != 2 || videos[0].Caption != "2: video" || videos[1].Caption != "4: mixta" {
		t.Fatalf("videos (incluye la mixta): %d de %d", len(videos), vt)
	}
	photos, pt, _ := svc.Page(shop, 1, 50, PostKindPhoto)
	if pt != 33 {
		t.Fatalf("fotos (todas menos la que solo trae video): %d", pt)
	}
	for _, p := range photos {
		if p.Caption == "2: video" {
			t.Fatal("la publicación solo de video no debe salir en «fotos»")
		}
	}
	if none, nt, err := svc.Page(uuid.New(), 1, 30, PostKindAll); err != nil || len(none) != 0 || nt != 0 {
		t.Fatalf("negocio sin publicaciones: %v %d %v", none, nt, err)
	}
	if far, _, _ := svc.Page(shop, 99, 30, PostKindAll); len(far) != 0 {
		t.Fatal("una página más allá del final viene vacía")
	}
}
