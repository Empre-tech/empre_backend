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
	err := db.Preload("Subcategories", func(db *gorm.DB) *gorm.DB {
		return db.Order("subcategories.name ASC")
	}).Limit(pageSize).Offset(offset).Find(&categories).Error
	return categories, total, err
}

func (r *CategoryRepository) FindByID(id uuid.UUID) (*models.Category, error) {
	var category models.Category
	err := r.DB.Preload("Subcategories", func(db *gorm.DB) *gorm.DB {
		return db.Order("subcategories.name ASC")
	}).First(&category, "id = ?", id).Error
	return &category, err
}

func (r *CategoryRepository) Update(category *models.Category) error {
	return r.DB.Save(category).Error
}

func (r *CategoryRepository) Delete(category *models.Category) error {
	return r.DB.Delete(category).Error
}

func (r *CategoryRepository) CreateSubcategory(sub *models.Subcategory) error {
	return r.DB.Create(sub).Error
}

func (r *CategoryRepository) FindSubcategoryByID(id uuid.UUID) (*models.Subcategory, error) {
	var sub models.Subcategory
	err := r.DB.First(&sub, "id = ?", id).Error
	return &sub, err
}

func (r *CategoryRepository) UpdateSubcategory(sub *models.Subcategory) error {
	return r.DB.Save(sub).Error
}

func (r *CategoryRepository) DeleteSubcategory(sub *models.Subcategory) error {
	return r.DB.Delete(sub).Error
}
