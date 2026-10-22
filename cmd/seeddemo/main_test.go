package main

import (
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

// Las fotos demo quedan en una publicación (3 fotos, la primera de perfil) y clearDemo las borra sin dejar medios.
func TestAddPhotos_CreatesAndClears(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no está definida")
	}
	schema := "seeddemo_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
		&models.Post{}, &models.EntityPhoto{}, &models.BusinessHour{}, &models.Offer{}, &models.OfferClaim{}, &models.OfferItem{},
		&models.Promotion{}); err != nil {
		t.Fatal(err)
	}

	owner := models.User{Name: "Dueño", Email: "o@x.co", PasswordHash: "x", Role: models.RoleUser}
	cat := models.Category{Name: "Educación", Color: "#1E5CA8"}
	db.Create(&owner)
	db.Create(&cat)
	e := models.Entity{ID: uuid.New(), OwnerID: owner.ID, Name: "Colegio", CategoryID: cat.ID, Latitude: 11.2, Longitude: -74.2}
	if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{files: map[string][]byte{}}
	if err := db.Transaction(func(tx *gorm.DB) error { return addPhotos(tx, store, &e, cat, time.Now()) }); err != nil {
		t.Fatal(err)
	}

	count := func(table string) (c int64) {
		db.Table(table).Where("entity_id = ?", e.ID).Count(&c)
		return
	}
	if len(store.files) != photosPerPost || count("entity_photos") != photosPerPost || count("posts") != 1 {
		t.Fatalf("archivos %d, fotos %d, publicaciones %d", len(store.files), count("entity_photos"), count("posts"))
	}
	var got models.Entity
	db.First(&got, "id = ?", e.ID)
	if got.ProfileMediaID == nil {
		t.Fatal("sin foto de perfil")
	}

	clearDemo(db, owner.ID)
	var left, entities int64
	db.Model(&models.Media{}).Count(&left)
	db.Model(&models.Entity{}).Count(&entities)
	if left != 0 || entities != 0 || count("posts") != 0 || count("entity_photos") != 0 {
		t.Fatalf("quedaron: medios %d, negocios %d, publicaciones %d, fotos %d", left, entities, count("posts"), count("entity_photos"))
	}
}
