package database

import (
	"log"

	"empre_backend/internal/models"

	"gorm.io/gorm"
)

// defaultCategories are created only when the categories table is empty,
// so a fresh database is usable right away (the app needs at least one
// category to register a business).
var defaultCategories = []string{
	"Restaurantes",
	"Cafeterías y postres",
	"Bares y vida nocturna",
	"Tiendas y comercio",
	"Belleza y peluquería",
	"Salud y bienestar",
	"Servicios del hogar",
	"Tecnología",
	"Turismo y hospedaje",
	"Educación",
	"Otros",
}

// SeedCategories inserts the default categories if none exist yet.
func SeedCategories(db *gorm.DB) {
	var count int64
	if err := db.Model(&models.Category{}).Count(&count).Error; err != nil {
		log.Println("Seed categories: could not count categories: ", err)
		return
	}
	if count > 0 {
		return
	}

	categories := make([]models.Category, 0, len(defaultCategories))
	for _, name := range defaultCategories {
		categories = append(categories, models.Category{Name: name})
	}
	if err := db.Create(&categories).Error; err != nil {
		log.Println("Seed categories: could not create categories: ", err)
		return
	}
	log.Printf("Seed categories: created %d default categories", len(categories))
}
