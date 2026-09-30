package database

import (
	"log"

	"empre_backend/internal/models"

	"gorm.io/gorm"
)

// defaultCategory pairs a category name with the Ionicons name used for its
// badge and for the map marker of every business in it.
type defaultCategory struct {
	Name string
	Icon string
}

// defaultCategories are created only when the categories table is empty,
// so a fresh database is usable right away (the app needs at least one
// category to register a business).
var defaultCategories = []defaultCategory{
	{"Restaurantes", "restaurant-outline"},
	{"Cafeterías y postres", "cafe-outline"},
	{"Bares y vida nocturna", "beer-outline"},
	{"Tiendas y comercio", "storefront-outline"},
	{"Belleza y peluquería", "cut-outline"},
	{"Salud y bienestar", "medkit-outline"},
	{"Servicios del hogar", "hammer-outline"},
	{"Tecnología", "hardware-chip-outline"},
	{"Turismo y hospedaje", "bed-outline"},
	{"Educación", "school-outline"},
	{"Automotriz", "car-outline"},
	{"Mascotas", "paw-outline"},
	{"Eventos y fiestas", "gift-outline"},
	{"Fotografía y diseño", "camera-outline"},
	{"Deporte y recreación", "basketball-outline"},
	{"Otros", "ellipsis-horizontal-circle-outline"},
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
	for _, c := range defaultCategories {
		categories = append(categories, models.Category{Name: c.Name, Icon: c.Icon})
	}
	if err := db.Create(&categories).Error; err != nil {
		log.Println("Seed categories: could not create categories: ", err)
		return
	}
	log.Printf("Seed categories: created %d default categories", len(categories))
}

// defaultSubcategories maps a Category name (as seeded above) to the
// subcategories it starts with. A category not listed here simply gets none;
// admins can add more later from the admin panel.
var defaultSubcategories = map[string][]string{
	"Restaurantes": {
		"Comida rápida",
		"Comida saludable / fit",
		"Comida internacional",
		"Comida típica",
		"Vegetariano / vegano",
		"A domicilio",
	},
	"Cafeterías y postres": {
		"Cafetería",
		"Panadería",
		"Repostería y postres",
		"Heladería",
	},
	"Bares y vida nocturna": {
		"Bar",
		"Discoteca",
		"Karaoke",
		"Billar / juegos",
	},
	"Tiendas y comercio": {
		"Ropa y accesorios",
		"Supermercado / minimarket",
		"Artesanías y regalos",
		"Electrónica",
		"Papelería",
		"Ferretería",
	},
	"Belleza y peluquería": {
		"Peluquería",
		"Barbería",
		"Spa y masajes",
		"Uñas",
	},
	"Salud y bienestar": {
		"Consultorio médico",
		"Odontología",
		"Gimnasio",
		"Farmacia",
	},
	"Servicios del hogar": {
		"Plomería",
		"Electricidad",
		"Limpieza",
		"Jardinería",
		"Mudanzas",
		"Fumigación",
	},
	"Tecnología": {
		"Reparación de celulares",
		"Reparación de computadores",
		"Accesorios tecnológicos",
	},
	"Turismo y hospedaje": {
		"Hotel",
		"Hostal",
		"Tours y excursiones",
		"Alquiler vacacional",
	},
	"Educación": {
		"Academia / instituto",
		"Clases particulares",
		"Idiomas",
	},
	"Automotriz": {
		"Taller mecánico",
		"Lavadero de carros",
		"Repuestos y accesorios",
		"Alquiler de vehículos",
	},
	"Mascotas": {
		"Veterinaria",
		"Peluquería canina",
		"Tienda de mascotas",
		"Guardería y paseo de mascotas",
	},
	"Eventos y fiestas": {
		"Decoración y alquiler",
		"Catering",
		"DJ y sonido",
		"Organización de eventos",
	},
	"Fotografía y diseño": {
		"Fotografía",
		"Diseño gráfico",
		"Impresión y publicidad",
	},
	"Deporte y recreación": {
		"Gimnasio / crossfit",
		"Clases y entrenadores",
		"Alquiler de canchas",
		"Tienda deportiva",
	},
}

// SeedSubcategories inserts the default subcategories if none exist yet. It
// runs after SeedCategories so the parent categories already have IDs.
func SeedSubcategories(db *gorm.DB) {
	var count int64
	if err := db.Model(&models.Subcategory{}).Count(&count).Error; err != nil {
		log.Println("Seed subcategories: could not count subcategories: ", err)
		return
	}
	if count > 0 {
		return
	}

	var categories []models.Category
	if err := db.Find(&categories).Error; err != nil {
		log.Println("Seed subcategories: could not load categories: ", err)
		return
	}

	var subcategories []models.Subcategory
	for _, cat := range categories {
		names, ok := defaultSubcategories[cat.Name]
		if !ok {
			continue
		}
		for _, name := range names {
			subcategories = append(subcategories, models.Subcategory{Name: name, CategoryID: cat.ID})
		}
	}

	if len(subcategories) == 0 {
		return
	}
	if err := db.Create(&subcategories).Error; err != nil {
		log.Println("Seed subcategories: could not create subcategories: ", err)
		return
	}
	log.Printf("Seed subcategories: created %d default subcategories", len(subcategories))
}

// ResetCatalog permanently deletes every business (and everything hanging off
// it: chat messages, reviews, favorites, photos, its subcategory links) plus
// every category and subcategory, so the catalog can start clean with the
// default categories/icons above. Intended to be run once, from the
// `resetcatalog` command — never from the API — since it is destructive and
// has no confirmation step of its own.
//
// It does not delete the underlying S3 media objects, only the database
// rows; orphaned files can be cleaned up separately if needed.
func ResetCatalog(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := deleteAllBusinessData(tx); err != nil {
			return err
		}

		// Then the catalog itself.
		if err := tx.Exec("DELETE FROM subcategories").Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM categories").Error; err != nil {
			return err
		}

		return nil
	})
}

// deleteAllBusinessData deletes every business (Entity) and everything that
// hangs off one, in the order their foreign keys require. Factored out of
// ResetCatalog so ResetEverythingExceptAdmins can reuse it without also
// touching categories/subcategories (those two reset functions disagree on
// whether the catalog itself should survive).
//
// NOTE: subscriptions, subscription_payments and business_hours used to be
// missing from this cleanup (ResetCatalog predates all three features), so a
// reset used to leave orphaned rows behind referencing deleted entities.
// Fixed here.
func deleteAllBusinessData(tx *gorm.DB) error {
	if err := tx.Exec("DELETE FROM messages").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM reviews").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM favorites").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM entity_subcategories").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM entity_photos").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM posts").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM business_hours").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM subscription_payments").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM subscriptions").Error; err != nil {
		return err
	}
	if err := tx.Exec("DELETE FROM entities").Error; err != nil {
		return err
	}
	return nil
}

// ResetEverythingExceptAdmins wipes the whole database back to a blank
// slate — every business and everything hanging off it (deleteAllBusinessData,
// same as ResetCatalog), the catalog (categories/subcategories), and every
// user account that is NOT models.RoleAdmin, along with that user's own
// tokens. Admin accounts (and the default categories/icons, reseeded right
// after by SeedCategories/SeedSubcategories once the app starts back up) are
// the only things left standing.
//
// Like ResetCatalog, this is meant to be run once, deliberately, from a
// one-off command (never from the API) — see cmd/resetall. It does not touch
// the underlying S3 media objects, only database rows.
func ResetEverythingExceptAdmins(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := deleteAllBusinessData(tx); err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM subcategories").Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM categories").Error; err != nil {
			return err
		}

		// Tokens of the users we're about to delete.
		nonAdmin := "(SELECT id FROM users WHERE role <> 'admin')"
		if err := tx.Exec("DELETE FROM refresh_tokens WHERE user_id IN " + nonAdmin).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM push_tokens WHERE user_id IN " + nonAdmin).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM password_reset_tokens WHERE user_id IN " + nonAdmin).Error; err != nil {
			return err
		}

		// Finally, the non-admin users themselves.
		if err := tx.Exec("DELETE FROM users WHERE role <> 'admin'").Error; err != nil {
			return err
		}

		return nil
	})
}
