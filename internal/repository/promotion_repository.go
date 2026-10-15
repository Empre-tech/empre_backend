package repository

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type PromotionRepository struct {
	DB *gorm.DB
}

func NewPromotionRepository(db *gorm.DB) *PromotionRepository {
	return &PromotionRepository{DB: db}
}

func (r *PromotionRepository) Create(p *models.Promotion) error {
	return r.DB.Create(p).Error
}

func (r *PromotionRepository) Update(p *models.Promotion) error {
	return r.DB.Save(p).Error
}

func (r *PromotionRepository) FindByID(id uuid.UUID) (*models.Promotion, error) {
	var p models.Promotion
	err := r.DB.First(&p, "id = ?", id).Error
	return &p, err
}

func (r *PromotionRepository) FindByEntity(entityID uuid.UUID) ([]models.Promotion, error) {
	var ps []models.Promotion
	err := r.DB.Where("entity_id = ?", entityID).Order("created_at DESC").Find(&ps).Error
	return ps, err
}

// CountActiveByEntity cuenta las promociones activas de un negocio (límite).
func (r *PromotionRepository) CountActiveByEntity(entityID uuid.UUID, now time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.Promotion{}).
		Where("entity_id = ? AND status = ? AND ends_at > ?", entityID, models.PromotionActive, now).
		Count(&count).Error
	return count, err
}

// FindActive devuelve las promociones activas (con su entidad) para el
// listado de la pestaña Ofertas.
func (r *PromotionRepository) FindActive(now time.Time) ([]models.Promotion, error) {
	var ps []models.Promotion
	err := r.DB.Preload("Entity.Category").
		Where("status = ? AND ends_at > ?", models.PromotionActive, now).
		Order("ends_at ASC").
		Find(&ps).Error
	return ps, err
}

// FindActiveByEntity devuelve las promociones activas de un negocio (perfil).
func (r *PromotionRepository) FindActiveByEntity(entityID uuid.UUID, now time.Time) ([]models.Promotion, error) {
	var ps []models.Promotion
	err := r.DB.Where("entity_id = ? AND status = ? AND ends_at > ?", entityID, models.PromotionActive, now).
		Order("ends_at ASC").Find(&ps).Error
	return ps, err
}
