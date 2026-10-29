package services

import (
	"errors"
	"log"

	"empre_backend/internal/ledger"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"sync"

	"github.com/google/uuid"
)

type EntityService struct {
	Repo         *repository.EntityRepository
	MediaService *MediaService
	// Zones asigna la zona con nombre al guardar; si es nil, no se asigna.
	Zones *ZoneService
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
	if !ledger.ValidKind(entity.BusinessProfile) {
		entity.BusinessProfile = s.CategoryKind(entity.CategoryID)
	}
	s.assignZone(entity)
	return s.Repo.Create(entity)
}

func (s *EntityService) UpdateEntity(entity *models.Entity) error {
	s.assignZone(entity)
	return s.Repo.Update(entity)
}

// CategoryKind es el tipo de negocio que sugiere una categoría (su campo profile; si está vacío, por su nombre).
func (s *EntityService) CategoryKind(categoryID uuid.UUID) string {
	var c models.Category
	if err := s.Repo.DB.Select("id", "name", "profile").First(&c, "id = ?", categoryID).Error; err != nil {
		return ledger.KindOther
	}
	if ledger.ValidKind(c.Profile) {
		return c.Profile
	}
	if k := ledger.KindForName(c.Name); k != "" {
		return k
	}
	return ledger.KindOther
}

// FollowCategoryProfile: si el negocio cambió de categoría y su tipo era el que sugería la anterior (el dueño no lo
// había cambiado a mano), toma el de la nueva. Si el dueño lo había elegido, se respeta.
func (s *EntityService) FollowCategoryProfile(e *models.Entity, prevCategory uuid.UUID) {
	if e.CategoryID == prevCategory {
		return
	}
	if !ledger.ValidKind(e.BusinessProfile) || e.BusinessProfile == s.CategoryKind(prevCategory) {
		e.BusinessProfile = s.CategoryKind(e.CategoryID)
	}
}

// assignZone no impide guardar el negocio si falla: la zona es un dato de ayuda.
func (s *EntityService) assignZone(entity *models.Entity) {
	if s.Zones == nil {
		return
	}
	if err := s.Zones.Assign(entity); err != nil {
		log.Printf("no se pudo asignar la zona al negocio %s: %v", entity.ID, err)
	}
}

func (s *EntityService) FindByID(id uuid.UUID) (*models.Entity, error) {
	entity, err := s.Repo.FindByID(id)
	if err == nil {
		s.populateMediaURLs(entity)
	}
	return entity, err
}

func (s *EntityService) FindAll(lat, long, radius float64, categoryID, subcategoryID, query string, openNow bool, page, pageSize int) ([]repository.EntityListRow, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	entities, total, err := s.Repo.FindAll(lat, long, radius, categoryID, subcategoryID, query, openNow, page, pageSize)
	if err == nil {
		// El listado solo muestra la foto de perfil, que ya viene unida a la consulta. populateMediaURLs no sirve aquí:
		// sin la portada precargada la buscaría negocio por negocio (una consulta más por cada uno).
		var wg sync.WaitGroup
		for i := range entities {
			if entities[i].ProfileMedia == nil {
				continue
			}
			wg.Add(1)
			go func(e *models.Entity) {
				defer wg.Done()
				s.MediaService.PopulateURL(e.ProfileMedia)
				e.ProfileURL = e.ProfileMedia.URL
			}(&entities[i].Entity)
		}
		wg.Wait()
	}
	return entities, total, err
}

// FindByBBox returns businesses inside a visible rectangle (plus the total
// count), with media URLs populated. Used by discovery.
// FindByBBox es el listado del mapa y de Descubre. No firma enlaces (3.29): el perfil, la portada y las fotos salen con
// la ruta estable /api/images/{id} (ProfilePath, CoverURL, attachRecentPhotos). Antes se firmaban hasta 2 enlaces por
// negocio (perfil y banner) en cada petición y, como cambiaban siempre, el teléfono volvía a bajar las miniaturas.
func (s *EntityService) FindByBBox(minLat, maxLat, minLng, maxLng float64, categoryID, subcategoryID, query string, cap int) ([]models.Entity, int64, error) {
	return s.Repo.FindByBBox(minLat, maxLat, minLng, maxLng, categoryID, subcategoryID, query, cap)
}

func (s *EntityService) FindAllByOwner(ownerID uuid.UUID, page, pageSize int) ([]models.Entity, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	entities, total, err := s.Repo.FindAllByOwner(ownerID, page, pageSize)
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

func (s *EntityService) DeleteEntity(entity *models.Entity) error {
	return s.Repo.Delete(entity)
}

// SetEntitySubcategories replaces an entity's subcategory associations.
func (s *EntityService) SetEntitySubcategories(entity *models.Entity, subcategoryIDs []uuid.UUID) error {
	return s.Repo.SetSubcategories(entity, subcategoryIDs)
}

// SetEntityHours replaces an entity's weekly schedule.
func (s *EntityService) SetEntityHours(entity *models.Entity, hours []models.BusinessHour) error {
	return s.Repo.SetHours(entity, hours)
}

// FindAllByStatus lists entities filtered by moderation status (Admin only).
func (s *EntityService) FindAllByStatus(status models.VerificationStatus, page, pageSize int) ([]models.Entity, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	entities, total, err := s.Repo.FindAllByStatus(status, page, pageSize)
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

// SetVerificationStatus updates an entity's moderation status (Admin only).
// "verified" also turns on the public verified badge; any other status turns it off.
func (s *EntityService) SetVerificationStatus(id uuid.UUID, status models.VerificationStatus) (*models.Entity, error) {
	entity, err := s.Repo.FindByID(id)
	if err != nil {
		return nil, err
	}

	entity.VerificationStatus = status
	entity.IsVerified = status == models.StatusVerified
	if err := s.Repo.Update(entity); err != nil {
		return nil, err
	}

	// Re-fetch so the response carries freshly loaded associations/media.
	updated, err := s.Repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	s.populateMediaURLs(updated)
	return updated, nil
}

// PopulateMediaURLs fills in ProfileURL/BannerURL and gallery media URLs for
// a slice of already-fetched entities (e.g. a favorites listing).
func (s *EntityService) PopulateMediaURLs(entities []models.Entity) {
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
