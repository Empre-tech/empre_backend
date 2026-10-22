package services

import (
	"sort"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// OfferListService alimenta la pestaña Ofertas: relámpagos y promociones
// activos, filtrados por categoría y ordenados por "near" (distancia) o
// "trending" (urgencia; con tendencias en Fase 3 se reemplaza por "lo más buscado").
type OfferListService struct {
	OfferRepo     *repository.OfferRepository
	PromotionRepo *repository.PromotionRepository
}

func NewOfferListService(offerRepo *repository.OfferRepository, promotionRepo *repository.PromotionRepository) *OfferListService {
	return &OfferListService{OfferRepo: offerRepo, PromotionRepo: promotionRepo}
}

func (s *OfferListService) List(lat, long float64, categoryID, order string) ([]models.Offer, []models.Promotion, error) {
	now := utils.NowInCartagena()

	offers, err := s.OfferRepo.FindActive(now)
	if err != nil {
		return nil, nil, err
	}
	promotions, err := s.PromotionRepo.FindActive(now)
	if err != nil {
		return nil, nil, err
	}

	if categoryID != "" {
		catID, err := uuid.Parse(categoryID)
		if err == nil {
			offers = filterOffersByCategory(offers, catID)
			promotions = filterPromotionsByCategory(promotions, catID)
		}
	}

	switch order {
	case "trending":
		sort.SliceStable(offers, func(i, j int) bool { return offers[i].EndsAt.Before(offers[j].EndsAt) })
		sort.SliceStable(promotions, func(i, j int) bool { return promotions[i].EndsAt.Before(promotions[j].EndsAt) })
	default: // near
		sort.SliceStable(offers, func(i, j int) bool {
			return s.distance(lat, long, &offers[i].Entity) < s.distance(lat, long, &offers[j].Entity)
		})
		sort.SliceStable(promotions, func(i, j int) bool {
			return s.distance(lat, long, &promotions[i].Entity) < s.distance(lat, long, &promotions[j].Entity)
		})
	}

	return offers, promotions, nil
}

func (s *OfferListService) distance(lat, long float64, e *models.Entity) float64 {
	if (lat == 0 && long == 0) || e == nil {
		return 0
	}
	return utils.DistanceMeters(lat, long, e.Latitude, e.Longitude)
}

func filterOffersByCategory(offers []models.Offer, categoryID uuid.UUID) []models.Offer {
	out := offers[:0]
	for _, o := range offers {
		if o.Entity.CategoryID == categoryID {
			out = append(out, o)
		}
	}
	return out
}

func filterPromotionsByCategory(promotions []models.Promotion, categoryID uuid.UUID) []models.Promotion {
	out := promotions[:0]
	for _, p := range promotions {
		if p.Entity.CategoryID == categoryID {
			out = append(out, p)
		}
	}
	return out
}
