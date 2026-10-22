package repository

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MaxCategoryFilter es cuántas categorías acepta un filtro a la vez (un grupo de la app tiene pocas).
const MaxCategoryFilter = 20

// ParseCategoryIDs lee el parámetro `category`: una categoría o varias separadas por comas (un grupo entero).
// Descarta los valores que no son un identificador y los repetidos, y corta en MaxCategoryFilter.
// `asked` dice si se pidió algún filtro (aunque ningún valor sirva).
func ParseCategoryIDs(raw string) (ids []uuid.UUID, asked bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	seen := map[uuid.UUID]bool{}
	for _, part := range strings.Split(raw, ",") {
		id, err := uuid.Parse(strings.TrimSpace(part))
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		if ids = append(ids, id); len(ids) == MaxCategoryFilter {
			break
		}
	}
	return ids, true
}

// WhereCategories aplica el filtro de categoría sobre `column`. Pedir un filtro cuyos valores no sirven devuelve
// cero negocios (no todos, ni un error del motor por un uuid inválido).
func WhereCategories(db *gorm.DB, column, raw string) *gorm.DB {
	ids, asked := ParseCategoryIDs(raw)
	switch {
	case !asked:
		return db
	case len(ids) == 0:
		return db.Where("1 = 0")
	case len(ids) == 1:
		return db.Where(column+" = ?", ids[0])
	default:
		return db.Where(column+" IN ?", ids)
	}
}
