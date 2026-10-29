package services

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Variantes (fase 7): un producto con sabores, tallas o tamaños. Cada variante es un ítem del catálogo con su propia
// existencia; el producto «padre» no se vende ni se cuenta: agrupa. Una variante sin precio o sin costo usa los del padre.

// MaxVariants por producto.
const MaxVariants = 30

// VariantSep une el nombre del producto con el de la variante: «Proteína 2 lb · Chocolate».
const VariantSep = " · "

// variantInfo indexa una lista de ítems activos: por id y cuántas variantes activas tiene cada padre.
func variantInfo(items []models.ServiceItem) (map[uuid.UUID]models.ServiceItem, map[uuid.UUID]int) {
	byID := make(map[uuid.UUID]models.ServiceItem, len(items))
	children := map[uuid.UUID]int{}
	for _, it := range items {
		byID[it.ID] = it
		if it.ParentID != nil {
			children[*it.ParentID]++
		}
	}
	return byID, children
}

// effectivePrice: el precio de la variante o, si no tiene, el de su producto.
func effectivePrice(it models.ServiceItem, byID map[uuid.UUID]models.ServiceItem) *int {
	if it.Price == nil && it.ParentID != nil {
		if p, ok := byID[*it.ParentID]; ok {
			return p.Price
		}
	}
	return it.Price
}

// effectiveCost: el costo de la variante o, si no tiene, el de su producto.
func effectiveCost(it models.ServiceItem, byID map[uuid.UUID]models.ServiceItem) *int {
	if it.Cost == nil && it.ParentID != nil {
		if p, ok := byID[*it.ParentID]; ok {
			return p.Cost
		}
	}
	return it.Cost
}

// hasVariants dice si un ítem tiene variantes activas.
func hasVariants(db *gorm.DB, itemID uuid.UUID) (bool, error) {
	var n int64
	err := db.Model(&models.ServiceItem{}).Where("parent_id = ? AND status = ?", itemID, models.ServiceActive).Count(&n).Error
	return n > 0, err
}

// syncParentSoldOut: un producto con variantes se ve agotado solo si TODAS sus variantes lo están.
func syncParentSoldOut(db *gorm.DB, parentID uuid.UUID) error {
	return db.Exec(`UPDATE service_items SET sold_out = NOT EXISTS (
		SELECT 1 FROM service_items v WHERE v.parent_id = ? AND v.status = 'active' AND v.sold_out = false)
		WHERE id = ? AND EXISTS (SELECT 1 FROM service_items v WHERE v.parent_id = ? AND v.status = 'active')`, parentID, parentID, parentID).Error
}
