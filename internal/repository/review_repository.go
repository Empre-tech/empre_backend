package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ReviewRepository struct {
	DB *gorm.DB
}

func NewReviewRepository(db *gorm.DB) *ReviewRepository {
	return &ReviewRepository{DB: db}
}

// Upsert creates the caller's review for an entity, or updates it if one
// already exists (one review per user per business).
func (r *ReviewRepository) Upsert(entityID, userID uuid.UUID, rating int, comment string, claimID *uuid.UUID) (*models.Review, error) {
	var review models.Review
	err := r.DB.Where("entity_id = ? AND user_id = ?", entityID, userID).First(&review).Error
	if err == nil {
		review.Rating = rating
		review.Comment = comment
		// Editar el texto no quita la verificación; un canje nuevo sí la actualiza.
		if claimID != nil {
			review.ClaimID = claimID
		}
		if err := r.DB.Save(&review).Error; err != nil {
			return nil, err
		}
		return &review, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	review = models.Review{EntityID: entityID, UserID: userID, Rating: rating, Comment: comment, ClaimID: claimID}
	if err := r.DB.Create(&review).Error; err != nil {
		return nil, err
	}
	return &review, nil
}

// ReviewFilter acota la lista de reseñas de un negocio. El resumen y la distribución de notas no dependen del filtro.
type ReviewFilter struct {
	// Stars: solo esa nota (1 a 5); 0 = todas.
	Stars int
	// Verified: solo las respaldadas por un canje confirmado.
	Verified bool
	// WithComment: solo las que traen un comentario (no vacío ni solo espacios).
	WithComment bool
}

// MaxReviewPage es el tope de reseñas por página.
const MaxReviewPage = 50

func (r *ReviewRepository) FindByEntity(entityID uuid.UUID, page, pageSize int) ([]models.Review, int64, error) {
	return r.FindByEntityFiltered(entityID, page, pageSize, ReviewFilter{})
}

// FindByEntityFiltered lista las reseñas de un negocio, las más recientes primero, con el filtro dado; el total
// es el de las que cumplen el filtro.
func (r *ReviewRepository) FindByEntityFiltered(entityID uuid.UUID, page, pageSize int, f ReviewFilter) ([]models.Review, int64, error) {
	var reviews []models.Review
	var total int64
	if pageSize > MaxReviewPage {
		pageSize = MaxReviewPage
	}

	db := r.DB.Model(&models.Review{}).Where("entity_id = ?", entityID)
	if f.Stars >= 1 && f.Stars <= 5 {
		db = db.Where("rating = ?", f.Stars)
	}
	if f.Verified {
		db = db.Where("claim_id IS NOT NULL")
	}
	if f.WithComment {
		db = db.Where("btrim(comment) <> ''")
	}
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Joins("User").Order("reviews.created_at DESC").Limit(pageSize).Offset(offset).Find(&reviews).Error
	return reviews, total, err
}

func (r *ReviewRepository) FindByEntityAndUser(entityID, userID uuid.UUID) (*models.Review, error) {
	var review models.Review
	if err := r.DB.Where("entity_id = ? AND user_id = ?", entityID, userID).First(&review).Error; err != nil {
		return nil, err
	}
	return &review, nil
}

func (r *ReviewRepository) Delete(entityID, userID uuid.UUID) error {
	return r.DB.Where("entity_id = ? AND user_id = ?", entityID, userID).Delete(&models.Review{}).Error
}

// reviewSummaryRow scans the aggregate query result below.
type reviewSummaryRow struct {
	Avg   float64
	Count int64
}

// Summary returns the average rating and review count for an entity.
func (r *ReviewRepository) Summary(entityID uuid.UUID) (float64, int64, error) {
	var result reviewSummaryRow
	err := r.DB.Model(&models.Review{}).
		Select("COALESCE(AVG(rating), 0) as avg, COUNT(*) as count").
		Where("entity_id = ?", entityID).
		Scan(&result).Error
	return result.Avg, result.Count, err
}

// Breakdown devuelve cuántas reseñas hay de cada calificación (índice 0 = 1
// estrella … índice 4 = 5 estrellas) y cuántas están verificadas por un canje.
func (r *ReviewRepository) Breakdown(entityID uuid.UUID) ([5]int64, int64, error) {
	var distribution [5]int64
	var rows []struct {
		Rating int
		Count  int64
	}
	if err := r.DB.Model(&models.Review{}).
		Select("rating, COUNT(*) as count").
		Where("entity_id = ?", entityID).
		Group("rating").Scan(&rows).Error; err != nil {
		return distribution, 0, err
	}
	for _, row := range rows {
		if row.Rating >= 1 && row.Rating <= 5 {
			distribution[row.Rating-1] = row.Count
		}
	}
	var verified int64
	err := r.DB.Model(&models.Review{}).Where("entity_id = ? AND claim_id IS NOT NULL", entityID).Count(&verified).Error
	return distribution, verified, err
}

// ReviewSummary is one row of a bulk rating/count aggregation, keyed by EntityID.
type ReviewSummary struct {
	EntityID uuid.UUID `gorm:"column:entity_id"`
	Avg      float64   `gorm:"column:avg"`
	Count    int64     `gorm:"column:count"`
}

// Summaries returns the average rating and review count for several entities
// in a single query (used by list endpoints to avoid N+1 lookups).
func (r *ReviewRepository) Summaries(entityIDs []uuid.UUID) (map[uuid.UUID]ReviewSummary, error) {
	result := make(map[uuid.UUID]ReviewSummary, len(entityIDs))
	if len(entityIDs) == 0 {
		return result, nil
	}

	var rows []ReviewSummary
	err := r.DB.Model(&models.Review{}).
		Select("entity_id, COALESCE(AVG(rating), 0) as avg, COUNT(*) as count").
		Where("entity_id IN ?", entityIDs).
		Group("entity_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		result[row.EntityID] = row
	}
	return result, nil
}
