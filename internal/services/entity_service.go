package services

import (
	"errors"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

type EntityService struct {
	Repo         *repository.EntityRepository
	MediaService *MediaService
}

func NewEntityService(repo *repository.EntityRepository, mediaService *MediaService) *EntityService {
	return &EntityService{
		Repo:         repo,
		MediaService: mediaService,
	}
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
	entity, err := s.Repo.FindByID(id)
	if err == nil {
		s.populateMediaURLs(entity)
	}
	return entity, err
}

func (s *EntityService) FindAll(lat, long, radius float64, categoryID string) ([]models.Entity, error) {
	entities, err := s.Repo.FindAll(lat, long, radius, categoryID)
	if err == nil {
		for i := range entities {
			s.populateMediaURLs(&entities[i])
		}
	}
	return entities, err
}

func (s *EntityService) FindAllByOwner(ownerID uuid.UUID) ([]models.Entity, error) {
	entities, err := s.Repo.FindAllByOwner(ownerID)
	if err == nil {
		for i := range entities {
			s.populateMediaURLs(&entities[i])
		}
	}
	return entities, err
}

func (s *EntityService) DeleteEntity(entity *models.Entity) error {
	return s.Repo.Delete(entity)
}

func (s *EntityService) populateMediaURLs(e *models.Entity) {
	if e == nil {
		return
	}
	// Prepend BaseURL to relative paths for consistency
	if e.ProfileURL != "" && e.ProfileURL[0] == '/' {
		e.ProfileURL = s.MediaService.BaseURL + e.ProfileURL
	}
	if e.BannerURL != "" && e.BannerURL[0] == '/' {
		e.BannerURL = s.MediaService.BaseURL + e.BannerURL
	}

	for i := range e.Photos {
		s.MediaService.PopulateURL(&e.Photos[i].Media)
	}
}
