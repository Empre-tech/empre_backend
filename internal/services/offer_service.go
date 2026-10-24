package services

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

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
	OwnerNote    string
	EndsAt       time.Time
	TotalSlots   int
	RadiusMeters int
	RepeatPolicy models.RepeatPolicy
	// ServiceItemIDs son servicios del catálogo del mismo negocio que incluye la oferta (hasta MaxOfferItems);
	// SpecialPrice es el precio temporal propio. Ambos son opcionales.
	ServiceItemIDs []uuid.UUID
	SpecialPrice   *int
}

// OwnerNoteMax es el largo máximo de la nota de quien atiende (cabe en dos líneas de la tarjeta, 3.48).
const OwnerNoteMax = 90

// MaxOfferItems es el tope de servicios que incluye una oferta.
const MaxOfferItems = 5

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

	if utf8.RuneCountInString(strings.TrimSpace(in.OwnerNote)) > OwnerNoteMax {
		return nil, errors.New("la nota admite hasta 90 caracteres")
	}
	items, reference, err := s.freezeItems(entityID, in.ServiceItemIDs)
	if err != nil {
		return nil, err
	}
	if in.SpecialPrice != nil {
		if *in.SpecialPrice < 0 || *in.SpecialPrice > MaxServicePrice {
			return nil, errors.New("el precio especial debe estar entre 0 y 10.000.000")
		}
		if reference != nil && *in.SpecialPrice >= *reference {
			return nil, errors.New("el precio especial debe ser menor al precio habitual de lo incluido")
		}
	}

	offer := &models.Offer{
		ID:             uuid.New(),
		EntityID:       entityID,
		SpecialPrice:   in.SpecialPrice,
		ReferencePrice: reference,
		Items:          items,
		Type:           in.Type,
		Title:          in.Title,
		Price:          in.Price,
		NormalPrice:    in.NormalPrice,
		Conditions:     in.Conditions,
		OwnerNote:      strings.TrimSpace(in.OwnerNote),
		StartsAt:       now,
		EndsAt:         in.EndsAt,
		TotalSlots:     in.TotalSlots,
		RadiusMeters:   radius,
		RepeatPolicy:   repeat,
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
	if strings.TrimSpace(in.OwnerNote) == "" {
		in.OwnerNote = old.OwnerNote
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
	// Mismos servicios y precio especial que la oferta anterior, si no se piden otros. Los servicios se congelan
	// de nuevo con su precio habitual de HOY; uno ya retirado simplemente no vuelve.
	if in.ServiceItemIDs == nil {
		var prev []models.OfferItem
		if err := s.OfferRepo.DB.Where("offer_id = ? AND service_item_id IS NOT NULL", old.ID).Order("position ASC").Find(&prev).Error; err != nil {
			return nil, err
		}
		ids := make([]uuid.UUID, 0, len(prev))
		for _, it := range prev {
			ids = append(ids, *it.ServiceItemID)
		}
		var active []models.ServiceItem
		if len(ids) > 0 {
			if err := s.OfferRepo.DB.Where("id IN ? AND entity_id = ? AND status = ?", ids, old.EntityID, models.ServiceActive).Find(&active).Error; err != nil {
				return nil, err
			}
		}
		still := map[uuid.UUID]bool{}
		for _, a := range active {
			still[a.ID] = true
		}
		in.ServiceItemIDs = []uuid.UUID{}
		for _, id := range ids {
			if still[id] {
				in.ServiceItemIDs = append(in.ServiceItemIDs, id)
			}
		}
		if in.SpecialPrice == nil {
			in.SpecialPrice = old.SpecialPrice
		}
	}
	return s.publish(old.EntityID, in)
}

// freezeItems carga los servicios pedidos (deben ser del mismo negocio y estar activos), los copia congelados y
// calcula el precio de referencia: la suma de los precios habituales, solo si todos tienen precio.
func (s *OfferService) freezeItems(entityID uuid.UUID, ids []uuid.UUID) ([]models.OfferItem, *int, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	seen := map[uuid.UUID]bool{}
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) > MaxOfferItems {
		return nil, nil, errors.New("una oferta incluye hasta 5 servicios")
	}
	var found []models.ServiceItem
	if err := s.OfferRepo.DB.Where("id IN ? AND entity_id = ? AND status = ?", unique, entityID, models.ServiceActive).Find(&found).Error; err != nil {
		return nil, nil, err
	}
	byID := make(map[uuid.UUID]models.ServiceItem, len(found))
	for _, f := range found {
		byID[f.ID] = f
	}
	items := make([]models.OfferItem, 0, len(unique))
	total, allPriced := 0, true
	for i, id := range unique {
		svc, ok := byID[id]
		if !ok {
			return nil, nil, errors.New("un servicio no está en tu catálogo o ya fue retirado")
		}
		sid := svc.ID
		items = append(items, models.OfferItem{ServiceItemID: &sid, Name: svc.Name, Price: svc.Price, Position: i})
		if svc.Price == nil {
			allPriced = false
		} else {
			total += *svc.Price
		}
	}
	if !allPriced || total <= 0 {
		return items, nil, nil
	}
	return items, &total, nil
}

func (s *OfferService) ListByEntity(entityID, ownerID uuid.UUID) ([]models.Offer, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	return s.OfferRepo.FindByEntity(entityID)
}

// ActiveListForEntity devuelve todas las ofertas vigentes y con cupos de un negocio (hasta el tope), las que
// terminan antes primero.
func (s *OfferService) ActiveListForEntity(entityID uuid.UUID) ([]models.Offer, error) {
	list, _, err := s.OfferRepo.FindActiveByEntity(entityID, utils.NowInCartagena(), 0)
	return list, err
}

// ActivePageForEntity es lo mismo con tope: hasta `limit` ofertas (0 = todas hasta el tope) y cuántas hay en total.
func (s *OfferService) ActivePageForEntity(entityID uuid.UUID, limit int) ([]models.Offer, int64, error) {
	return s.OfferRepo.FindActiveByEntity(entityID, utils.NowInCartagena(), limit)
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
