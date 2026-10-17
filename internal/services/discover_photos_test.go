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

// Descubre muestra las fotos más recientes de cada negocio: de la publicación más nueva a la más vieja (y en su
// orden dentro de cada una), sin videos, hasta 4, y cuántas tiene en total.
func TestDiscoverPhotos_RecentFirstWithoutVideos(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cat := models.Category{Name: "Cafeterías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(name string, lat float64) uuid.UUID {
		e := models.Entity{OwnerID: owner, Name: name, CategoryID: cat.ID, Latitude: lat, Longitude: -74.2}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	shop := mk("Café", 11.2)
	mk("Sin fotos", 11.2005)
	now := time.Now()
	// post crea una publicación de hace `ago` horas con archivos «nombre:tipo», en el orden dado.
	post := func(entity uuid.UUID, ago int, files ...string) {
		p := models.Post{EntityID: entity, CreatedAt: now.Add(-time.Duration(ago) * time.Hour), UpdatedAt: now}
		if err := db.Omit(clause.Associations).Create(&p).Error; err != nil {
			t.Fatal(err)
		}
		// Se insertan al revés para que mande el orden, no la inserción.
		for i := len(files) - 1; i >= 0; i-- {
			var name, ct string
			fmt.Sscanf(files[i], "%s %s", &name, &ct)
			m := models.Media{S3Key: name, ContentType: ct}
			if err := db.Create(&m).Error; err != nil {
				t.Fatal(err)
			}
			ph := models.EntityPhoto{PostID: p.ID, EntityID: entity, MediaID: m.ID, Order: i}
			if err := db.Omit(clause.Associations).Create(&ph).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	post(shop, 48, "vieja image/jpeg")
	post(shop, 1, "nueva-video video/mp4", "nueva-1 image/jpeg", "nueva-2 image/png")
	post(shop, 24, "ayer-1 image/jpeg", "ayer-2 image/webp")
	post(shop, 72, "muy-vieja image/jpeg")

	list, _, err := repository.NewEntityRepository(db).FindByBBox(11.1, 11.3, -74.3, -74.1, "", "", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*models.Entity{}
	for i := range list {
		// Sin almacenamiento en la prueba: la URL es la clave (en producción la pone el servicio de medios).
		for j := range list[i].Photos {
			list[i].Photos[j].Media.URL = list[i].Photos[j].Media.S3Key
		}
		byName[list[i].Name] = &list[i]
	}

	urls, count := PhotoSample(byName["Café"], PhotoSampleMax)
	want := []string{"nueva-1", "nueva-2", "ayer-1", "ayer-2"}
	if fmt.Sprint(urls) != fmt.Sprint(want) {
		t.Fatalf("fotos: %v, quería %v (más recientes primero, sin el video, hasta 4)", urls, want)
	}
	if count != 6 {
		t.Fatalf("total de fotos: %d, quería 6 (el video no cuenta)", count)
	}
	if urls, count := PhotoSample(byName["Café"], 2); len(urls) != 2 || count != 6 {
		t.Fatalf("con tope 2: %v de %d", urls, count)
	}
	if urls, count := PhotoSample(byName["Sin fotos"], PhotoSampleMax); urls != nil || count != 0 {
		t.Fatalf("sin fotos: %v de %d", urls, count)
	}

	// Una foto sin URL (no se pudo firmar) no se manda ni se cuenta.
	e := models.Entity{Photos: []models.EntityPhoto{{Media: models.Media{ContentType: "image/jpeg"}}, {Media: models.Media{ContentType: "image/jpeg", URL: "ok"}}}}
	if urls, count := PhotoSample(&e, 4); fmt.Sprint(urls) != "[ok]" || count != 1 {
		t.Fatalf("sin URL: %v de %d", urls, count)
	}
}
