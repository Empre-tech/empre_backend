package services

import (
	"errors"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"sync"

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

func (s *EntityService) FindAll(lat, long, radius float64, categoryID string, page, pageSize int) ([]models.Entity, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	entities, total, err := s.Repo.FindAll(lat, long, radius, categoryID, page, pageSize)
	if err == nil {
		var wg sync.WaitGroup
		for i := range entities {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				s.populateMediaURLs(&entities[index])
			}(i)
		}
		wg.Wait()
	}
	return entities, total, err
}

func (s *EntityService) FindAllByOwner(ownerID uuid.UUID) ([]models.Entity, error) {
	entities, err := s.Repo.FindAllByOwner(ownerID)
	if err == nil {
		var wg sync.WaitGroup
		for i := range entities {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				s.populateMediaURLs(&entities[index])
			}(i)
		}
		wg.Wait()
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

	var wg sync.WaitGroup

	// 1. Profile Image
	if e.ProfileMediaID != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.ProfileMedia != nil {
				s.MediaService.PopulateURL(e.ProfileMedia)
				e.ProfileURL = e.ProfileMedia.URL
			} else {
				media, err := s.MediaService.Repo.FindByID(*e.ProfileMediaID)
				if err == nil {
					s.MediaService.PopulateURL(media)
					e.ProfileURL = media.URL
				}
			}
		}()
	}

	// 2. Banner Image
	if e.BannerMediaID != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.BannerMedia != nil {
				s.MediaService.PopulateURL(e.BannerMedia)
				e.BannerURL = e.BannerMedia.URL
			} else {
				media, err := s.MediaService.Repo.FindByID(*e.BannerMediaID)
				if err == nil {
					s.MediaService.PopulateURL(media)
					e.BannerURL = media.URL
				}
			}
		}()
	}

	// 3. Gallery Photos
	for i := range e.Photos {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.MediaService.PopulateURL(&e.Photos[idx].Media)
		}(i)
	}

	wg.Wait()
}
