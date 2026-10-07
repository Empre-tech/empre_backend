package services

import (
	"sort"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// DiscoveryItem is one ranked result: the entity plus the derived signals
// used to order it and render its pin/card.
type DiscoveryItem struct {
	Entity         models.Entity
	DistanceMeters float64
	AvgRating      float64
	ReviewCount    int64
	ActiveOffer    *models.Offer
	IsOpenNow      bool
	Popularity     float64
}

// RankingService ranks and diversifies discovery results. The client never
// reorders: the order returned here is authoritative.
type RankingService struct {
	EntityService *EntityService
	ReviewService *ReviewService
	OfferRepo     *repository.OfferRepository
	SignalService *SignalService
}

func NewRankingService(entityService *EntityService, reviewService *ReviewService, offerRepo *repository.OfferRepository, signalService *SignalService) *RankingService {
	return &RankingService{EntityService: entityService, ReviewService: reviewService, OfferRepo: offerRepo, SignalService: signalService}
}

const maxPerCategory = 2

// Discover returns the N best businesses for the visible area, ordered and
// diversified. `offerFilter` is "", "now", "today" or "week".
func (s *RankingService) Discover(lat, long, radius float64, categoryID, subcategoryID, query, offerFilter string, limit int) ([]DiscoveryItem, error) {
	if limit <= 0 {
		limit = 15
	}

	now := utils.NowInCartagena()
	// Fetch a generous window (no open-now filter: closed businesses still
	// come back as a last resort, and ranking puts them last).
	entities, _, err := s.EntityService.FindAll(lat, long, radius, categoryID, subcategoryID, query, false, 1, 200)
	if err != nil {
		return nil, err
	}

	entityIDs := make([]uuid.UUID, len(entities))
	for i := range entities {
		entityIDs[i] = entities[i].ID
	}

	ratings, _ := s.ReviewService.Summaries(entityIDs)
	offers, _ := s.OfferRepo.FindActiveForEntities(entityIDs, now)

	var popularity map[uuid.UUID]float64
	if s.SignalService != nil {
		popularity = s.SignalService.PopularityForEntities(entityIDs)
	}

	items := make([]DiscoveryItem, 0, len(entities))
	for i := range entities {
		e := &entities[i]
		dist := 0.0
		if lat != 0 || long != 0 {
			dist = utils.DistanceMeters(lat, long, e.Latitude, e.Longitude)
		}
		items = append(items, DiscoveryItem{
			Entity:         *e,
			DistanceMeters: dist,
			AvgRating:      ratings[e.ID].Avg,
			ReviewCount:    ratings[e.ID].Count,
			ActiveOffer:    offers[e.ID],
			IsOpenNow:      utils.IsOpenNow(e.Hours, now),
			Popularity:     popularity[e.ID],
		})
	}

	// Optional offer-type filter.
	if offerFilter != "" {
		filtered := items[:0]
		for _, it := range items {
			if it.ActiveOffer != nil && offerMatchesFilter(it.ActiveOffer.Type, offerFilter) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.IsOpenNow != b.IsOpenNow {
			return a.IsOpenNow
		}
		hasOfferA, hasOfferB := a.ActiveOffer != nil, b.ActiveOffer != nil
		if hasOfferA != hasOfferB {
			return hasOfferA
		}
		if a.DistanceMeters != b.DistanceMeters {
			return a.DistanceMeters < b.DistanceMeters
		}
		if a.AvgRating != b.AvgRating {
			return a.AvgRating > b.AvgRating
		}
		if a.Popularity != b.Popularity {
			return a.Popularity > b.Popularity
		}
		return a.Entity.Name < b.Entity.Name
	})

	// Diversify: at most `maxPerCategory` per category.
	diversified := make([]DiscoveryItem, 0, limit)
	categoryCount := map[uuid.UUID]int{}
	for _, it := range items {
		if categoryCount[it.Entity.CategoryID] >= maxPerCategory {
			continue
		}
		diversified = append(diversified, it)
		categoryCount[it.Entity.CategoryID]++
		if len(diversified) >= limit {
			break
		}
	}

	return diversified, nil
}

func offerMatchesFilter(typ models.OfferType, filter string) bool {
	switch filter {
	case "now":
		return typ == models.OfferHours
	case "today":
		return typ == models.OfferDay
	case "week":
		return typ == models.OfferWeek
	default:
		return true
	}
}
