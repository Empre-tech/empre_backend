package main

import (
	"bytes"
	"image/jpeg"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// fakeStore guarda en memoria lo que se «sube».
type fakeStore struct{ files map[string][]byte }

func (f *fakeStore) UploadFile(name string, body io.Reader, _ string) error {
	b, err := io.ReadAll(body)
	f.files[name] = b
	return err
}

// Las fotos dibujadas son JPEG válidos de 960×720 y cambian con la variante.
func TestSamplePhoto(t *testing.T) {
	for _, x := range extras {
		a, b := samplePhoto(x.top, x.bottom, x.motif, 0), samplePhoto(x.top, x.bottom, x.motif, 1)
		img, err := jpeg.Decode(bytes.NewReader(a))
		if err != nil {
			t.Fatalf("%s: no es JPEG: %v", x.motif, err)
		}
		if r := img.Bounds(); r.Dx() != photoW || r.Dy() != photoH {
			t.Fatalf("%s: tamaño %v", x.motif, r)
		}
		if bytes.Equal(a, b) {
			t.Fatalf("%s: dos variantes iguales", x.motif)
		}
	}
	if len(extras) != len(shops) {
		t.Fatalf("hay %d contenidos de ejemplo para %d negocios", len(extras), len(shops))
	}
	if got := slug("La Sazón de Rosa"); got != "la-sazon-de-rosa" {
		t.Fatalf("slug: %q", got)
	}
}

// El contenido de ejemplo se crea completo (publicaciones con fotos en su orden, foto de perfil, reseñas, segunda
// oferta y ofertas terminadas) y volver a correr el seed lo borra sin dejar huérfanos.
func TestAddExtras_CreatesAndClears(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no está definida")
	}
	schema := "seedqr_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error })
	if err := db.AutoMigrate(&models.User{}, &models.Media{}, &models.Category{}, &models.Subcategory{}, &models.Place{}, &models.Entity{},
		&models.Post{}, &models.EntityPhoto{}, &models.BusinessHour{}, &models.Offer{}, &models.OfferClaim{}, &models.Review{},
		&models.Favorite{}, &models.ServiceItem{}, &models.OfferItem{}); err != nil {
		t.Fatal(err)
	}

	owner := models.User{Name: "Dueño", Email: "o@x.co", PasswordHash: "x", Role: models.RoleUser}
	cat := models.Category{Name: "Restaurantes"}
	db.Create(&owner)
	db.Create(&cat)
	reviewers := make([]models.User, len(reviewerNames))
	for i := range reviewers {
		reviewers[i] = models.User{Name: reviewerNames[i], Email: uuid.NewString() + "@x.co", PasswordHash: "x", Role: models.RoleUser}
		db.Create(&reviewers[i])
	}
	e := models.Entity{ID: uuid.New(), OwnerID: owner.ID, Name: "La Sazón de Rosa", CategoryID: cat.ID, Latitude: 11.2, Longitude: -74.2}
	if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{files: map[string][]byte{}}
	now := time.Now()
	x := extras[0]
	var n int
	err = db.Transaction(func(tx *gorm.DB) error {
		var err error
		n, err = addExtras(tx, &e, x, now, store, reviewers, "")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	want := 0
	for _, p := range x.posts {
		want += p.photos
	}
	count := func(table string) (c int64) {
		db.Table(table).Where("entity_id = ?", e.ID).Count(&c)
		return
	}
	if n != want || len(store.files) != want || count("entity_photos") != int64(want) {
		t.Fatalf("fotos: subidas %d, archivos %d, filas %d; quería %d", n, len(store.files), count("entity_photos"), want)
	}
	if count("posts") != int64(len(x.posts)) || count("reviews") != int64(len(x.reviews)) {
		t.Fatalf("publicaciones %d, reseñas %d", count("posts"), count("reviews"))
	}
	var active, ended int64
	db.Model(&models.Offer{}).Where("entity_id = ? AND ends_at > ?", e.ID, now).Count(&active)
	db.Model(&models.Offer{}).Where("entity_id = ? AND ends_at <= ? AND used_slots > 0", e.ID, now).Count(&ended)
	if active != 1 || ended != int64(len(x.past)) {
		t.Fatalf("ofertas: %d vigentes (quería la segunda), %d terminadas", active, ended)
	}
	var got models.Entity
	db.First(&got, "id = ?", e.ID)
	if got.ProfileMediaID == nil {
		t.Fatal("sin foto de perfil")
	}
	// La publicación más reciente es la primera de la lista.
	var newest models.Post
	db.Where("entity_id = ?", e.ID).Order("created_at DESC").First(&newest)
	if newest.Caption != x.posts[0].caption {
		t.Fatalf("la más reciente: %q", newest.Caption)
	}

	// Volver a correr: se borra todo lo del dueño, también los medios.
	if err := db.Transaction(func(tx *gorm.DB) error { return clearOwner(tx, owner.ID) }); err != nil {
		t.Fatal(err)
	}
	var left int64
	db.Model(&models.Media{}).Count(&left)
	if left != 0 || count("posts") != 0 || count("entity_photos") != 0 || count("reviews") != 0 {
		t.Fatalf("quedaron: medios %d, publicaciones %d, fotos %d, reseñas %d", left, count("posts"), count("entity_photos"), count("reviews"))
	}
}
