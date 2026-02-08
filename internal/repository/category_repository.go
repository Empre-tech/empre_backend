package repository

import (
	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CategoryRepository struct {
	DB *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) *CategoryRepository {
	return &CategoryRepository{DB: db}
}

func (r *CategoryRepository) Create(category *models.Category) error {
	return r.DB.Create(category).Error
}

func (r *CategoryRepository) FindAll(page, pageSize int) ([]models.Category, int64, error) {
	var categories []models.Category
	var total int64

	db := r.DB.Model(&models.Category{})
	db.Count(&total)

	offset := (page - 1) * pageSize
	err := db.Limit(pageSize).Offset(offset).Find(&categories).Error
	return categories, total, err
}

func (r *CategoryRepository) FindByID(id uuid.UUID) (*models.Category, error) {
	var category models.Category
	err := r.DB.First(&category, "id = ?", id).Error
	return &category, err
}

func (r *CategoryRepository) Update(category *models.Category) error {
	return r.DB.Save(category).Error
}

func (r *CategoryRepository) Delete(category *models.Category) error {
	return r.DB.Delete(category).Error
}
