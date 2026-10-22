package repository

import (
	"strings"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SearchRepository implementa la búsqueda tolerante por categoría o texto
// (unaccent + sinónimos + pg_trgm) y las consultas por área para el endpoint
// de búsqueda por niveles y los chips dinámicos.
type SearchRepository struct {
	DB *gorm.DB
}

func NewSearchRepository(db *gorm.DB) *SearchRepository {
	return &SearchRepository{DB: db}
}

// MatchCategory encuentra la categoría que corresponde al término:
// 1) nombre o slug exacto (sin tildes), 2) sinónimo, 3) similitud difusa.
// Devuelve nil si no hay coincidencia.
func (r *SearchRepository) MatchCategory(q string) (*models.Category, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil, nil
	}

	var cat models.Category

	// 1. Nombre o slug exactos, sin tildes y sin distinguir mayúsculas.
	err := r.DB.Where(
		"unaccent(lower(name)) = unaccent(lower(?)) OR unaccent(lower(slug)) = unaccent(lower(?))",
		q, q,
	).First(&cat).Error
	if err == nil {
		return &cat, nil
	}

	// 2. Sinónimo (plural/singular incluidos en el seed).
	err = r.DB.Where(
		"EXISTS (SELECT 1 FROM jsonb_array_elements_text(synonyms) s WHERE unaccent(lower(s)) = unaccent(lower(?)))",
		q,
	).First(&cat).Error
	if err == nil {
		return &cat, nil
	}

	// 3. Similitud difusa (errores de escritura).
	err = r.DB.Where("similarity(unaccent(lower(name)), unaccent(lower(?))) > 0.3", q).
		First(&cat).Error
	if err != nil {
		return nil, nil
	}
	return &cat, nil
}

// FindByCategoryInBBox devuelve negocios con la categoría primaria en el rectángulo.
func (r *SearchRepository) FindByCategoryInBBox(categoryID uuid.UUID, minLat, maxLat, minLng, maxLng float64, limit int) ([]models.Entity, error) {
	var entities []models.Entity
	err := r.DB.Joins("Category").Preload("Hours").
		Where("entities.category_id = ?", categoryID).
		Where("entities.latitude BETWEEN ? AND ? AND entities.longitude BETWEEN ? AND ?", minLat, maxLat, minLng, maxLng).
		Limit(limit).
		Find(&entities).Error
	return entities, err
}

// FindByTextInBBox busca por nombre, descripción o etiquetas (tolerante).
func (r *SearchRepository) FindByTextInBBox(q string, minLat, maxLat, minLng, maxLng float64, limit int) ([]models.Entity, error) {
	var entities []models.Entity
	like := "%" + q + "%"
	err := r.DB.Joins("Category").Preload("Hours").
		Where(
			"unaccent(lower(entities.name)) LIKE unaccent(lower(?)) OR "+
				"unaccent(lower(entities.description)) LIKE unaccent(lower(?)) OR "+
				"EXISTS (SELECT 1 FROM jsonb_array_elements_text(entities.tags) t WHERE unaccent(lower(t)) LIKE unaccent(lower(?)))",
			like, like, like,
		).
		Where("entities.latitude BETWEEN ? AND ? AND entities.longitude BETWEEN ? AND ?", minLat, maxLat, minLng, maxLng).
		Limit(limit).
		Find(&entities).Error
	return entities, err
}

// FindRelatedCategories devuelve categorías relacionadas con la dada: misma
// familia (si tiene) o relación explícita con peso >= umbral.
func (r *SearchRepository) FindRelatedCategories(categoryID uuid.UUID, minWeight float64) ([]models.Category, error) {
	var cat models.Category
	if err := r.DB.First(&cat, "id = ?", categoryID).Error; err != nil {
		return nil, err
	}

	query := r.DB.Where(
		"id <> ? AND (id IN (SELECT to_id FROM category_relations WHERE from_id = ? AND weight >= ?))",
		categoryID, categoryID, minWeight,
	)
	if cat.Family != "" {
		query = query.Or("family = ?", cat.Family)
	}

	var cats []models.Category
	if err := query.Limit(5).Find(&cats).Error; err != nil {
		return nil, err
	}
	return cats, nil
}

// FindByCategoriesInBBox devuelve negocios de varias categorías en el rectángulo.
func (r *SearchRepository) FindByCategoriesInBBox(categoryIDs []uuid.UUID, minLat, maxLat, minLng, maxLng float64, limit int) ([]models.Entity, error) {
	var entities []models.Entity
	err := r.DB.Joins("Category").Preload("Hours").
		Where("entities.category_id IN ?", categoryIDs).
		Where("entities.latitude BETWEEN ? AND ? AND entities.longitude BETWEEN ? AND ?", minLat, maxLat, minLng, maxLng).
		Limit(limit).
		Find(&entities).Error
	return entities, err
}

// FindActiveOffersByCategories devuelve relámpagos activos de negocios de las
// categorías dadas dentro del rectángulo.
func (r *SearchRepository) FindActiveOffersByCategories(categoryIDs []uuid.UUID, minLat, maxLat, minLng, maxLng float64, now time.Time, limit int) ([]models.Offer, error) {
	var offers []models.Offer
	err := r.DB.Preload("Entity.Category").
		Joins("JOIN entities ON entities.id = offers.entity_id").
		Where("entities.category_id IN ?", categoryIDs).
		Where("offers.ends_at > ? AND offers.used_slots < offers.total_slots", now).
		Where("entities.latitude BETWEEN ? AND ? AND entities.longitude BETWEEN ? AND ?", minLat, maxLat, minLng, maxLng).
		Order("offers.ends_at ASC").
		Limit(limit).
		Find(&offers).Error
	return offers, err
}

// CategoryCountRow es una fila del conteo de negocios por categoría en un área.
type CategoryCountRow struct {
	CategoryID   uuid.UUID `gorm:"column:category_id"`
	Count        int64     `gorm:"column:count"`
}

// CountByCategoryInBBox cuenta negocios por categoría primaria en el rectángulo.
func (r *SearchRepository) CountByCategoryInBBox(minLat, maxLat, minLng, maxLng float64) ([]CategoryCountRow, error) {
	var rows []CategoryCountRow
	err := r.DB.Model(&models.Entity{}).
		Select("category_id, COUNT(*) as count").
		Where("latitude BETWEEN ? AND ? AND longitude BETWEEN ? AND ?", minLat, maxLat, minLng, maxLng).
		Group("category_id").
		Order("count DESC").
		Scan(&rows).Error
	return rows, err
}
