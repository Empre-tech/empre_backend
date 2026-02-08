package services

import (
	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type CategoryService struct {
	categoryRepo *repository.CategoryRepository
}

func NewCategoryService(categoryRepo *repository.CategoryRepository) *CategoryService {
	return &CategoryService{categoryRepo: categoryRepo}
}

func (s *CategoryService) Create(category *models.Category) error {
	return s.categoryRepo.Create(category)
}

func (s *CategoryService) FindAll(page, pageSize int) ([]models.Category, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return s.categoryRepo.FindAll(page, pageSize)
}

func (s *CategoryService) FindByID(id uuid.UUID) (*models.Category, error) {
	return s.categoryRepo.FindByID(id)
}

func (s *CategoryService) Update(category *models.Category) error {
	return s.categoryRepo.Update(category)
}

func (s *CategoryService) Delete(category *models.Category) error {
	return s.categoryRepo.Delete(category)
}
