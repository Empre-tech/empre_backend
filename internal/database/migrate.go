package database

import (
	"log"
	"strings"
	"unicode"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EnableSearchExtensions habilita unaccent y pg_trgm para la búsqueda
// tolerante (sin tildes, difusa). Idempotente.
func EnableSearchExtensions(db *gorm.DB) error {
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS unaccent").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm").Error; err != nil {
		return err
	}
	log.Println("Búsqueda tolerante: extensiones unaccent + pg_trgm habilitadas")
	return nil
}

// Slugify convierte un nombre a slug: minúsculas, sin tildes, espacios a guiones.
func Slugify(s string) string {
	var b strings.Builder
	replacer := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	)
	s = strings.ToLower(strings.TrimSpace(replacer.Replace(s)))
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// MigrateHierarchy convierte las subcategorías en categorías hijas
// (categories.parent_id) y rellena business_categories: primarias desde
// entities.category_id y secundarias desde entity_subcategories. Idempotente.
func MigrateHierarchy(db *gorm.DB) error {
	// 1. Subcategorías → categorías hijas.
	var existingChildren int64
	db.Model(&models.Category{}).Where("parent_id IS NOT NULL").Count(&existingChildren)
	if existingChildren == 0 {
		var subs []models.Subcategory
		if err := db.Find(&subs).Error; err != nil {
			return err
		}
		for i := range subs {
			s := subs[i]
			var parent models.Category
			if err := db.First(&parent, "id = ?", s.CategoryID).Error; err != nil {
				continue
			}
			child := models.Category{
				ID:       uuid.New(),
				Name:     s.Name,
				Icon:     parent.Icon,
				Color:    parent.Color,
				Family:   parent.Family,
				ParentID: &s.CategoryID,
				Slug:     Slugify(s.Name),
			}
			if err := db.Create(&child).Error; err != nil {
				return err
			}
		}
	}

	// 2. business_categories (idempotente: si ya hay filas, no rehacer).
	var bc int64
	db.Model(&models.BusinessCategory{}).Count(&bc)
	if bc > 0 {
		return nil
	}

	var entities []models.Entity
	if err := db.Unscoped().Find(&entities).Error; err != nil {
		return err
	}
	for i := range entities {
		e := entities[i]
		if e.CategoryID == uuid.Nil {
			continue
		}
		row := models.BusinessCategory{BusinessID: e.ID, CategoryID: e.CategoryID, IsPrimary: true}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}

	type escRow struct {
		EntityID      uuid.UUID `gorm:"column:entity_id"`
		SubcategoryID uuid.UUID `gorm:"column:subcategory_id"`
	}
	var escRows []escRow
	if err := db.Raw("SELECT entity_id, subcategory_id FROM entity_subcategories").Scan(&escRows).Error; err != nil {
		return err
	}
	for _, r := range escRows {
		var sub models.Subcategory
		if err := db.First(&sub, "id = ?", r.SubcategoryID).Error; err != nil {
			continue
		}
		var child models.Category
		if err := db.Where("parent_id = ? AND name = ?", sub.CategoryID, sub.Name).First(&child).Error; err != nil {
			continue
		}
		row := models.BusinessCategory{BusinessID: r.EntityID, CategoryID: child.ID, IsPrimary: false}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return err
		}
	}

	log.Println("Migración de jerarquía de categorías y business_categories completada")
	return nil
}

// MigratePlaces agrupa negocios cercanos (< 50 m) en un mismo Place y les
// asigna place_id. Idempotente (solo asigna los que aún no tienen).
func MigratePlaces(db *gorm.DB) error {
	var entities []models.Entity
	if err := db.Where("latitude <> 0 AND longitude <> 0 AND place_id IS NULL").Find(&entities).Error; err != nil {
		return err
	}

	var places []models.Place
	if err := db.Find(&places).Error; err != nil {
		return err
	}

	for i := range entities {
		e := entities[i]
		var target *models.Place
		for j := range places {
			p := places[j]
			if utils.DistanceMeters(e.Latitude, e.Longitude, p.Latitude, p.Longitude) < 50 {
				target = &p
				break
			}
		}
		if target == nil {
			created := models.Place{ID: uuid.New(), Latitude: e.Latitude, Longitude: e.Longitude}
			if err := db.Create(&created).Error; err != nil {
				return err
			}
			places = append(places, created)
			target = &created
		}
		if err := db.Model(&models.Entity{}).Where("id = ?", e.ID).Update("place_id", target.ID).Error; err != nil {
			return err
		}
	}

	log.Println("Migración de places completada")
	return nil
}

// SeedFamilies asigna la familia de marca a las categorías según su color
// (calidez/comercio/bienestar/neutro), para el nivel "related" de la búsqueda.
// Idempotente: solo toca categorías sin familia.
func SeedFamilies(db *gorm.DB) error {
	familyByColor := map[string]string{
		"#F58220": "calidez",
		"#F59A4B": "calidez",
		"#D96A0C": "calidez",
		"#C97A2E": "calidez",
		"#2468C6": "comercio",
		"#1A4F9C": "comercio",
		"#3B78D1": "comercio",
		"#5A8FD8": "comercio",
		"#1E5CA8": "comercio",
		"#4A86D8": "comercio",
		"#0B8A8F": "bienestar",
		"#0F6E73": "bienestar",
		"#1FA2A8": "bienestar",
		"#2BA3A0": "bienestar",
		"#12848C": "bienestar",
		"#66768D": "neutro",
	}
	var cats []models.Category
	if err := db.Where("family = '' OR family IS NULL").Find(&cats).Error; err != nil {
		return err
	}
	for i := range cats {
		f, ok := familyByColor[cats[i].Color]
		if !ok {
			continue
		}
		if err := db.Model(&models.Category{}).Where("id = ?", cats[i].ID).Update("family", f).Error; err != nil {
			return err
		}
	}
	log.Println("Familias de categorías asignadas")
	return nil
}

// SeedSynonyms llena sinónimos por categoría para la búsqueda tolerante.
// Solo toca categorías que aún no tienen sinónimos. Idempotente.
func SeedSynonyms(db *gorm.DB) error {
	table := map[string][]string{
		"Restaurantes":         {"restaurante", "restaurant", "comida", "almuerzo", "cenar"},
		"Cafeterías y postres": {"cafeteria", "cafe", "café", "postre", "postres", "panaderia", "panadería", "heladeria"},
		"Bares y vida nocturna": {"bar", "discoteca", "rumba", "cerveza", "copas", "karaoke"},
		"Tiendas y comercio":    {"tienda", "almacen", "almacén", "comercio", "ropa", "supermercado"},
		"Belleza y peluquería":  {"peluqueria", "peluquería", "barberia", "barbería", "spa", "unas", "uñas", "corte"},
		"Salud y bienestar":     {"farmacia", "drogueria", "droguería", "medico", "médico", "doctor", "gimnasio"},
		"Servicios del hogar":   {"plomeria", "plomería", "electricista", "limpieza", "hogar", "jardineria"},
		"Tecnología":            {"tecnologia", "tecnología", "celulares", "computadores", "reparacion", "reparación", "telefono"},
		"Turismo y hospedaje":   {"hotel", "hostal", "turismo", "hospedaje", "alojamiento", "tour"},
		"Educación":             {"educacion", "educación", "clases", "academia", "colegio", "idiomas", "cursos"},
		"Automotriz":            {"taller", "mecanico", "mecánico", "lavadero", "carros", "motos", "repuestos"},
		"Mascotas":              {"veterinaria", "mascotas", "perros", "gatos", "peluqueria canina"},
		"Eventos y fiestas":     {"eventos", "fiestas", "decoracion", "decoración", "catering", "fiesta"},
		"Fotografía y diseño":   {"fotografia", "fotografía", "foto", "fotos", "diseño", "diseño grafico", "impresion"},
		"Deporte y recreación":  {"deporte", "gimnasio", "crossfit", "canchas", "entrenador", "deportes"},
		"Otros":                 {"otros", "varios", "diversos"},
	}
	for name, syns := range table {
		var cat models.Category
		if err := db.Where("name = ?", name).First(&cat).Error; err != nil {
			continue
		}
		if len(cat.Synonyms) > 0 {
			continue
		}
		cat.Synonyms = syns
		if err := db.Save(&cat).Error; err != nil {
			return err
		}
	}
	log.Println("Sinónimos de categorías sembrados")
	return nil
}
