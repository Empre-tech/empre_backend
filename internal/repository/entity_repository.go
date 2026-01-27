package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EntityRepository struct {
	DB *gorm.DB
}

func NewEntityRepository(db *gorm.DB) *EntityRepository {
	return &EntityRepository{DB: db}
}

func (r *EntityRepository) Create(entity *models.Entity) error {
	// Simple create, Lat/Long are now regular float columns
	return r.DB.Create(entity).Error
}

func (r *EntityRepository) Update(entity *models.Entity) error {
	return r.DB.Save(entity).Error
}

func (r *EntityRepository) FindByID(id uuid.UUID) (*models.Entity, error) {
	var entity models.Entity
	err := r.DB.Preload("Category").Preload("Photos.Media").First(&entity, "id = ?", id).Error
	return &entity, err
}

func (r *EntityRepository) FindAll(lat, long, radius float64, categoryID string) ([]models.Entity, error) {
	var entities []models.Entity

	db := r.DB.Model(&models.Entity{}).Preload("Category").Preload("Photos.Media")

	// Filter by Category
	if categoryID != "" {
		db = db.Where("category_id = ?", categoryID)
	}

	// Filter by Location - NAIVE IMPLEMENTATION (Bounding Box)
	if lat != 0 && long != 0 {
		if radius == 0 {
			radius = 5000 // 5km
		}

		// Approximate degrees: 1 degree ~= 111km
		degRadius := radius / 111000.0

		minLat := lat - degRadius
		maxLat := lat + degRadius
		minLong := long - degRadius
		maxLong := long + degRadius

		db = db.Where("latitude BETWEEN ? AND ?", minLat, maxLat).
			Where("longitude BETWEEN ? AND ?", minLong, maxLong)
	}

	err := db.Find(&entities).Error

	return entities, err
}

func (r *EntityRepository) FindAllByOwner(ownerID uuid.UUID) ([]models.Entity, error) {
	var entities []models.Entity
	err := r.DB.Preload("Category").Preload("Photos.Media").Where("owner_id = ?", ownerID).Find(&entities).Error
	return entities, err
}

func (r *EntityRepository) Delete(entity *models.Entity) error {
	return r.DB.Delete(entity).Error
}
