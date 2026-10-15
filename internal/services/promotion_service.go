package services

import (
	"errors"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// maxActivePromotions es el límite de promociones activas por negocio (decisión).
const maxActivePromotions = 3

type PromotionService struct {
	PromotionRepo *repository.PromotionRepository
	EntityRepo    *repository.EntityRepository
}

func NewPromotionService(promotionRepo *repository.PromotionRepository, entityRepo *repository.EntityRepository) *PromotionService {
	return &PromotionService{PromotionRepo: promotionRepo, EntityRepo: entityRepo}
}

type PublishPromotionInput struct {
	Title       string
	Description string
	Price       string
	Discount    string
	NormalPrice string
	StartsAt    time.Time
	EndsAt      time.Time
	Conditions  string
	Tags        string
}

func (s *PromotionService) ensureOwner(entityID, ownerID uuid.UUID) (*models.Entity, error) {
	entity, err := s.EntityRepo.FindByID(entityID)
	if err != nil {
		return nil, errors.New("negocio no encontrado")
	}
	if entity.OwnerID != ownerID {
		return nil, errors.New("no autorizado para este negocio")
	}
	return entity, nil
}

func (s *PromotionService) Publish(entityID, ownerID uuid.UUID, in PublishPromotionInput) (*models.Promotion, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	if in.Title == "" {
		return nil, errors.New("el título es obligatorio")
	}
	if in.Price == "" && in.Discount == "" {
		return nil, errors.New("indica un precio o un descuento")
	}

	now := utils.NowInCartagena()
	if !in.EndsAt.After(now) {
		return nil, errors.New("la fecha de fin debe ser futura")
	}

	count, err := s.PromotionRepo.CountActiveByEntity(entityID, now)
	if err != nil {
		return nil, err
	}
	if count >= maxActivePromotions {
		return nil, errors.New("límite de promociones activas alcanzado")
	}

	startsAt := in.StartsAt
	if startsAt.IsZero() {
		startsAt = now
	}

	p := &models.Promotion{
		ID:          uuid.New(),
		EntityID:    entityID,
		Title:       in.Title,
		Description: in.Description,
		Price:       in.Price,
		Discount:    in.Discount,
		NormalPrice: in.NormalPrice,
		StartsAt:    startsAt,
		EndsAt:      in.EndsAt,
		Conditions:  in.Conditions,
		Tags:        in.Tags,
		Status:      models.PromotionActive,
	}
	if err := s.PromotionRepo.Create(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Update edita campos opcionales y/o cambia el estado (pausar/finalizar).
func (s *PromotionService) Update(promotionID, ownerID uuid.UUID, in PublishPromotionInput, status string) (*models.Promotion, error) {
	p, err := s.PromotionRepo.FindByID(promotionID)
	if err != nil {
		return nil, errors.New("promoción no encontrada")
	}
	if _, err := s.ensureOwner(p.EntityID, ownerID); err != nil {
		return nil, err
	}

	if in.Title != "" {
		p.Title = in.Title
	}
	if in.Description != "" {
		p.Description = in.Description
	}
	if in.Price != "" || in.Discount != "" {
		p.Price = in.Price
		p.Discount = in.Discount
	}
	if in.NormalPrice != "" {
		p.NormalPrice = in.NormalPrice
	}
	if !in.EndsAt.IsZero() {
		p.EndsAt = in.EndsAt
	}
	if in.Conditions != "" {
		p.Conditions = in.Conditions
	}
	if in.Tags != "" {
		p.Tags = in.Tags
	}
	if status != "" {
		if !models.PromotionStatus(status).Valid() {
			return nil, errors.New("estado inválido")
		}
		p.Status = models.PromotionStatus(status)
	}

	if err := s.PromotionRepo.Update(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PromotionService) ListByEntity(entityID, ownerID uuid.UUID) ([]models.Promotion, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	return s.PromotionRepo.FindByEntity(entityID)
}

// ActiveForEntity devuelve las promociones activas de un negocio (perfil público).
func (s *PromotionService) ActiveForEntity(entityID uuid.UUID) ([]models.Promotion, error) {
	return s.PromotionRepo.FindActiveByEntity(entityID, utils.NowInCartagena())
}
