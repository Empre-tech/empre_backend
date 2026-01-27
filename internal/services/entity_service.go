package services

import (
	"errors"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type EntityService struct {
	Repo *repository.EntityRepository
}

func NewEntityService(repo *repository.EntityRepository) *EntityService {
	return &EntityService{Repo: repo}
}

func (s *EntityService) CreateEntity(entity *models.Entity) error {
	// Validate Owner? (Handled by Handler usually via Context)
	if entity.Name == "" {
		return errors.New("name is required")
	}
	return s.Repo.Create(entity)
}

func (s *EntityService) UpdateEntity(entity *models.Entity) error {
	return s.Repo.Update(entity)
}

func (s *EntityService) FindByID(id uuid.UUID) (*models.Entity, error) {
	return s.Repo.FindByID(id)
}

func (s *EntityService) FindAll(lat, long, radius float64, categoryID string) ([]models.Entity, error) {
	return s.Repo.FindAll(lat, long, radius, categoryID)
}

func (s *EntityService) DeleteEntity(entity *models.Entity) error {
	return s.Repo.Delete(entity)
}
