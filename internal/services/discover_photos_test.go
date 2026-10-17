package services

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Descubre muestra las fotos más recientes de cada negocio: de la publicación más nueva a la más vieja (y en su
// orden dentro de cada una), sin videos, hasta 4, y cuántas tiene en total. El listado del mapa trae solo esas 4 (no todas)
// y con la ruta estable /api/images/{id} en vez de un enlace firmado.
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
		byName[list[i].Name] = &list[i]
	}
	// Del id del medio a su nombre (la clave), para leer el orden.
	var media []models.Media
	db.Find(&media)
	nameOf := map[string]string{}
	for _, m := range media {
		nameOf[repository.ImagePath(m.ID)] = m.S3Key
	}

	cafe := byName["Café"]
	if len(cafe.Photos) != repository.RecentPhotosMax {
		t.Fatalf("el listado trae %d fotos del negocio; debe traer solo las %d más recientes", len(cafe.Photos), repository.RecentPhotosMax)
	}
	urls, count := PhotoSample(cafe, PhotoSampleMax)
	names := make([]string, len(urls))
	for i, u := range urls {
		if !strings.HasPrefix(u, "/api/images/") {
			t.Fatalf("URL %q: debe ser la ruta estable, no un enlace firmado", u)
		}
		names[i] = nameOf[u]
	}
	want := []string{"nueva-1", "nueva-2", "ayer-1", "ayer-2"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("fotos: %v, quería %v (más recientes primero, sin el video, hasta 4)", names, want)
	}
	if count != 6 {
		t.Fatalf("total de fotos: %d, quería 6 (el video no cuenta, aunque solo vengan 4)", count)
	}
	if urls, count := PhotoSample(cafe, 2); len(urls) != 2 || count != 6 {
		t.Fatalf("con tope 2: %v de %d", urls, count)
	}
	if urls, count := PhotoSample(byName["Sin fotos"], PhotoSampleMax); urls != nil || count != 0 {
		t.Fatalf("sin fotos: %v de %d", urls, count)
	}
	// La portada (sin banner) es una de esas fotos, también por la ruta estable.
	if c := CoverURL(cafe); !strings.HasPrefix(c, "/api/images/") {
		t.Fatalf("portada: %q", c)
	}

	// Una foto sin URL (no se pudo firmar) no se manda ni se cuenta.
	e := models.Entity{Photos: []models.EntityPhoto{{Media: models.Media{ContentType: "image/jpeg"}}, {Media: models.Media{ContentType: "image/jpeg", URL: "ok"}}}}
	if urls, count := PhotoSample(&e, 4); fmt.Sprint(urls) != "[ok]" || count != 1 {
		t.Fatalf("sin URL: %v de %d", urls, count)
	}
}

// El perfil y la portada salen con la ruta estable cuando el negocio tiene el medio guardado (3.29): el teléfono guarda
// la miniatura y el pin no se redibuja porque cambió un enlace firmado. Sin medio, lo que traiga el negocio.
func TestStableProfileAndCover(t *testing.T) {
	pid, bid := uuid.New(), uuid.New()
	e := models.Entity{ProfileMediaID: &pid, BannerMediaID: &bid, ProfileURL: "https://firmado/p?x=1", BannerURL: "https://firmado/b?x=1"}
	if got := ProfilePath(&e); got != "/api/images/"+pid.String() {
		t.Fatalf("perfil: %q", got)
	}
	if got := CoverURL(&e); got != "/api/images/"+bid.String() {
		t.Fatalf("portada: %q", got)
	}
	plain := models.Entity{ProfileURL: "https://otro/p.jpg"}
	if got := ProfilePath(&plain); got != "https://otro/p.jpg" {
		t.Fatalf("sin medio, lo que traiga: %q", got)
	}
	if got := ProfilePath(&models.Entity{}); got != "" {
		t.Fatalf("sin nada: %q", got)
	}
}
