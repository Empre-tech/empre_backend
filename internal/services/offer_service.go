package services

import (
	"errors"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// offerTypeRule holds the per-type configuration from the product spec
// (Backend v2 §4.2). A fourth type only needs a new entry here.
type offerTypeRule struct {
	maxActive int // active offers of this type per business
}

var offerTypeRules = map[models.OfferType]offerTypeRule{
	models.OfferHours: {maxActive: 2},
	models.OfferDay:   {maxActive: 1},
	models.OfferWeek:  {maxActive: 1},
}

const defaultRadiusMeters = 500

type OfferService struct {
	OfferRepo  *repository.OfferRepository
	EntityRepo *repository.EntityRepository
}

func NewOfferService(offerRepo *repository.OfferRepository, entityRepo *repository.EntityRepository) *OfferService {
	return &OfferService{OfferRepo: offerRepo, EntityRepo: entityRepo}
}

// PublishOfferInput is everything a business provides to publish (or
// republish) an offer.
type PublishOfferInput struct {
	Type         models.OfferType
	Title        string
	Price        string
	NormalPrice  string
	Conditions   string
	EndsAt       time.Time
	TotalSlots   int
	RadiusMeters int
	RepeatPolicy models.RepeatPolicy
}

func (s *OfferService) ensureOwner(entityID, ownerID uuid.UUID) (*models.Entity, error) {
	entity, err := s.EntityRepo.FindByID(entityID)
	if err != nil {
		return nil, errors.New("negocio no encontrado")
	}
	if entity.OwnerID != ownerID {
		return nil, errors.New("no autorizado para este negocio")
	}
	return entity, nil
}

func (s *OfferService) publish(entityID uuid.UUID, in PublishOfferInput) (*models.Offer, error) {
	if !in.Type.Valid() {
		return nil, errors.New("tipo de oferta inválido")
	}
	if in.Title == "" {
		return nil, errors.New("el título es obligatorio")
	}
	if in.TotalSlots <= 0 {
		return nil, errors.New("los cupos deben ser mayores a cero")
	}
	now := utils.NowInCartagena()
	if !in.EndsAt.After(now) {
		return nil, errors.New("la oferta ya venció")
	}

	count, err := s.OfferRepo.CountActiveByType(entityID, in.Type, now)
	if err != nil {
		return nil, err
	}
	if count >= int64(offerTypeRules[in.Type].maxActive) {
		return nil, errors.New("límite de ofertas activas para este tipo alcanzado")
	}

	radius := in.RadiusMeters
	if radius <= 0 {
		radius = defaultRadiusMeters
	}
	repeat := in.RepeatPolicy
	if repeat == "" {
		repeat = models.RepeatOnce
	}

	offer := &models.Offer{
		ID:           uuid.New(),
		EntityID:     entityID,
		Type:         in.Type,
		Title:        in.Title,
		Price:        in.Price,
		NormalPrice:  in.NormalPrice,
		Conditions:   in.Conditions,
		StartsAt:     now,
		EndsAt:       in.EndsAt,
		TotalSlots:   in.TotalSlots,
		RadiusMeters: radius,
		RepeatPolicy: repeat,
	}
	if err := s.OfferRepo.Create(offer); err != nil {
		return nil, err
	}
	return offer, nil
}

func (s *OfferService) Publish(entityID, ownerID uuid.UUID, in PublishOfferInput) (*models.Offer, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	return s.publish(entityID, in)
}

// QuickAction applies a fast action to an active offer. `action` is one of
// "extend" (value = minutes), "radius" (value = meters) or "slots" (value =
// number of slots to add). Never touches title/price/conditions.
func (s *OfferService) QuickAction(offerID, ownerID uuid.UUID, action string, value int) (*models.Offer, error) {
	offer, err := s.OfferRepo.FindByID(offerID)
	if err != nil {
		return nil, errors.New("oferta no encontrada")
	}
	if _, err := s.ensureOwner(offer.EntityID, ownerID); err != nil {
		return nil, err
	}

	switch action {
	case "extend":
		if value <= 0 {
			return nil, errors.New("minutos inválidos")
		}
		offer.EndsAt = offer.EndsAt.Add(time.Duration(value) * time.Minute)
	case "radius":
		if value <= 0 {
			return nil, errors.New("radio inválido")
		}
		offer.RadiusMeters = value
	case "slots":
		if value <= 0 {
			return nil, errors.New("cupos inválidos")
		}
		offer.TotalSlots += value
	default:
		return nil, errors.New("acción no soportada")
	}

	if err := s.OfferRepo.Update(offer); err != nil {
		return nil, err
	}
	return offer, nil
}

// Republish clones a previous offer into a new publication with fresh
// vigencia/slots from the request, copying the immutable content.
func (s *OfferService) Republish(oldOfferID, ownerID uuid.UUID, in PublishOfferInput) (*models.Offer, error) {
	old, err := s.OfferRepo.FindByID(oldOfferID)
	if err != nil {
		return nil, errors.New("oferta no encontrada")
	}
	if _, err := s.ensureOwner(old.EntityID, ownerID); err != nil {
		return nil, err
	}

	if in.Title == "" {
		in.Title = old.Title
	}
	if in.Price == "" {
		in.Price = old.Price
	}
	if in.NormalPrice == "" {
		in.NormalPrice = old.NormalPrice
	}
	if in.Conditions == "" {
		in.Conditions = old.Conditions
	}
	if in.Type == "" {
		in.Type = old.Type
	}
	if in.RadiusMeters <= 0 {
		in.RadiusMeters = old.RadiusMeters
	}
	if in.RepeatPolicy == "" {
		in.RepeatPolicy = old.RepeatPolicy
	}
	return s.publish(old.EntityID, in)
}

func (s *OfferService) ListByEntity(entityID, ownerID uuid.UUID) ([]models.Offer, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	return s.OfferRepo.FindByEntity(entityID)
}

// ActiveForEntity returns the first active offer for a business (public
// profile view). Returns nil when there is none.
func (s *OfferService) ActiveForEntity(entityID uuid.UUID) (*models.Offer, error) {
	offers, err := s.OfferRepo.FindByEntity(entityID)
	if err != nil {
		return nil, err
	}
	now := utils.NowInCartagena()
	for i := range offers {
		if offers[i].EndsAt.After(now) && offers[i].UsedSlots < offers[i].TotalSlots {
			return &offers[i], nil
		}
	}
	return nil, nil
}
