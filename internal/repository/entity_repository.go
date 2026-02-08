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
	err := r.DB.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Photos", func(db *gorm.DB) *gorm.DB {
		return db.Joins("Media")
	}).First(&entity, "entities.id = ?", id).Error
	return &entity, err
}

func (r *EntityRepository) FindAll(lat, long, radius float64, categoryID string, page, pageSize int) ([]models.Entity, int64, error) {
	var entities []models.Entity
	var total int64

	db := r.DB.Model(&models.Entity{}).Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Photos", func(db *gorm.DB) *gorm.DB {
		return db.Joins("Media")
	})

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

	// Count total records before applying pagination
	db.Count(&total)

	// Apply Pagination
	offset := (page - 1) * pageSize
	err := db.Limit(pageSize).Offset(offset).Find(&entities).Error

	return entities, total, err
}

func (r *EntityRepository) FindAllByOwner(ownerID uuid.UUID, page, pageSize int) ([]models.Entity, int64, error) {
	var entities []models.Entity
	var total int64

	db := r.DB.Model(&models.Entity{}).Where("entities.owner_id = ?", ownerID)
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Joins("Category").Joins("ProfileMedia").Joins("BannerMedia").Preload("Photos", func(db *gorm.DB) *gorm.DB {
		return db.Joins("Media")
	}).Limit(pageSize).Offset(offset).Find(&entities).Error

	return entities, total, err
}

func (r *EntityRepository) Delete(entity *models.Entity) error {
	return r.DB.Delete(entity).Error
}
