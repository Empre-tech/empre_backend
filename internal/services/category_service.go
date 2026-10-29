package services

import (
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

// categoriesTTL: las categorías casi no cambian y se piden en cada pantalla (3 consultas, ~300 ms con la base
// remota). Se guardan 5 minutos y cualquier cambio de categorías o subcategorías vacía la caché (3.53).
const categoriesTTL = 5 * time.Minute

type categoryPage struct {
	list  []models.Category
	total int64
}

type CategoryService struct {
	categoryRepo *repository.CategoryRepository
	pages        *TTLCache[[2]int, categoryPage]
}

func NewCategoryService(categoryRepo *repository.CategoryRepository) *CategoryService {
	return &CategoryService{categoryRepo: categoryRepo, pages: NewTTLCache[[2]int, categoryPage](categoriesTTL)}
}

// changed se llama después de cada escritura que tuvo éxito.
func (s *CategoryService) changed(err error) error {
	if err == nil {
		s.pages.Clear()
	}
	return err
}

func (s *CategoryService) Create(category *models.Category) error {
	return s.changed(s.categoryRepo.Create(category))
}

func (s *CategoryService) FindAll(page, pageSize int) ([]models.Category, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	key := [2]int{page, pageSize}
	if p, ok := s.pages.Get(key); ok {
		return p.list, p.total, nil
	}
	list, total, err := s.categoryRepo.FindAll(page, pageSize)
	if err == nil {
		s.pages.Set(key, categoryPage{list, total})
	}
	return list, total, err
}

func (s *CategoryService) FindByID(id uuid.UUID) (*models.Category, error) {
	return s.categoryRepo.FindByID(id)
}

func (s *CategoryService) Update(category *models.Category) error {
	return s.changed(s.categoryRepo.Update(category))
}

func (s *CategoryService) Delete(category *models.Category) error {
	return s.changed(s.categoryRepo.Delete(category))
}

func (s *CategoryService) CreateSubcategory(sub *models.Subcategory) error {
	return s.changed(s.categoryRepo.CreateSubcategory(sub))
}

func (s *CategoryService) FindSubcategoryByID(id uuid.UUID) (*models.Subcategory, error) {
	return s.categoryRepo.FindSubcategoryByID(id)
}

func (s *CategoryService) UpdateSubcategory(sub *models.Subcategory) error {
	return s.changed(s.categoryRepo.UpdateSubcategory(sub))
}

func (s *CategoryService) DeleteSubcategory(sub *models.Subcategory) error {
	return s.changed(s.categoryRepo.DeleteSubcategory(sub))
}
