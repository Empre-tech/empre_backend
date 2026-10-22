package database

import (
	"os"
	"testing"

	"empre_backend/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Auditoría de pines (22 oct): las categorías creadas sin color se completan al arrancar, sin pisar lo ya puesto.
func TestFillCategoryLooks(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL no definido")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public")
	db.Exec("CREATE EXTENSION IF NOT EXISTS pgcrypto")
	if err := db.AutoMigrate(&models.Category{}); err != nil {
		t.Fatal(err)
	}
	// Como una base vieja: categorías sin color; una con color propio que no debe cambiar; una que no es de las de siempre.
	db.Exec(`INSERT INTO categories (name, icon, color) VALUES ('Restaurantes', 'restaurant-outline', ''), ('Mascotas', '', NULL),
		('Tecnología', 'hardware-chip-outline', '#123456'), ('Inventada', 'star', '')`)

	SeedCategories(db) // hay categorías: no crea, solo completa

	look := func(name string) (string, string) {
		var c models.Category
		db.Where("name = ?", name).First(&c)
		return c.Color, c.Icon
	}
	if c, _ := look("Restaurantes"); c != "#F58220" {
		t.Fatalf("Restaurantes: %q", c)
	}
	if c, i := look("Mascotas"); c != "#2BA3A0" || i != "paw-outline" {
		t.Fatalf("Mascotas: %q %q", c, i)
	}
	if c, _ := look("Tecnología"); c != "#123456" {
		t.Fatalf("un color ya puesto no se toca: %q", c)
	}
	if c, _ := look("Inventada"); c != "" {
		t.Fatalf("una categoría desconocida no se inventa color: %q", c)
	}
	var n int64
	db.Model(&models.Category{}).Count(&n)
	if n != 4 {
		t.Fatalf("no debía crear categorías: %d", n)
	}
}
