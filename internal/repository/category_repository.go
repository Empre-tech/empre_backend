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

func (r *CategoryRepository) FindAll() ([]models.Category, error) {
	var categories []models.Category
	err := r.DB.Find(&categories).Error
	return categories, err
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
