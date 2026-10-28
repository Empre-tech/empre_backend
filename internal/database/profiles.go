package database

import (
	"log"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"

	"gorm.io/gorm"
)

// BackfillProfiles llena el tipo de negocio donde falte (idempotente: solo toca valores vacíos o inválidos).
//
//  1. Cada categoría sin `profile`: el de su padre si lo tiene; si no, el que se deduce de su nombre (o del de su
//     padre); si nada coincide, «other». Las que quedan en «other» se listan en el log para revisarlas a mano.
//  2. Cada negocio sin `business_profile`: el de su categoría.
//
// Devuelve los nombres de las categorías que quedaron en «other» por no reconocerse.
func BackfillProfiles(db *gorm.DB) ([]string, error) {
	var cats []models.Category
	if err := db.Select("id", "name", "parent_id", "profile").Find(&cats).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]*models.Category, len(cats))
	for i := range cats {
		byID[cats[i].ID.String()] = &cats[i]
	}
	var unknown []string
	var resolve func(c *models.Category, depth int) string
	resolve = func(c *models.Category, depth int) string {
		if ledger.ValidKind(c.Profile) {
			return c.Profile
		}
		var parent *models.Category
		if c.ParentID != nil {
			parent = byID[c.ParentID.String()]
		}
		kind := ""
		if parent != nil && depth < 5 {
			if pk := resolve(parent, depth+1); pk != ledger.KindOther {
				kind = pk
			}
		}
		if kind == "" {
			kind = ledger.KindForName(c.Name)
		}
		if kind == "" && parent != nil {
			kind = ledger.KindForName(parent.Name)
		}
		if kind == "" {
			kind = ledger.KindOther
			unknown = append(unknown, c.Name)
		}
		c.Profile = kind
		if err := db.Model(&models.Category{}).Where("id = ?", c.ID).Update("profile", kind).Error; err != nil {
			log.Printf("no se pudo guardar el tipo de la categoría %s: %v", c.Name, err)
		}
		return kind
	}
	for i := range cats {
		resolve(&cats[i], 0)
	}
	if len(unknown) > 0 {
		log.Printf("Categorías sin tipo de negocio reconocido (quedaron en «other», revísalas): %v", unknown)
	}

	err := db.Exec(`UPDATE entities SET business_profile = categories.profile
		FROM categories
		WHERE entities.category_id = categories.id
		  AND (entities.business_profile IS NULL OR entities.business_profile NOT IN ('shop','food','service','valuable','other'))
		  AND categories.profile IN ('shop','food','service','valuable','other')`).Error
	return unknown, err
}

// BackfillItemKinds le pone tipo (servicio o producto) a los ítems del catálogo que no lo tienen (idempotente).
//   - con duración o reservable → servicio (confirmado: la señal es clara);
//   - con inventario activo → producto (confirmado);
//   - sin señal: servicio si el negocio es de servicios, producto si no. Queda sin confirmar (kind_checked = false)
//     para que la app le pida al dueño revisarlo.
func BackfillItemKinds(db *gorm.DB) error {
	return db.Exec(`UPDATE service_items AS si SET
		kind = CASE
			WHEN si.bookable OR si.duration_min IS NOT NULL THEN 'service'
			WHEN EXISTS (SELECT 1 FROM stock_items st WHERE st.service_item_id = si.id AND st.archived = false AND st.mode <> 'none') THEN 'product'
			WHEN e.business_profile = 'service' THEN 'service'
			ELSE 'product' END,
		kind_checked = (si.bookable OR si.duration_min IS NOT NULL
			OR EXISTS (SELECT 1 FROM stock_items st WHERE st.service_item_id = si.id AND st.archived = false AND st.mode <> 'none'))
		FROM entities e
		WHERE e.id = si.entity_id AND si.kind NOT IN ('service', 'product')`).Error
}
