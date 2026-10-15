package services

import (
	"math"
	"sort"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// radiiMeters son los radios del nivel 2 de la búsqueda (2→5→10→25 km).
var radiiMeters = []float64{2000, 5000, 10000, 25000}

// relatedMinWeight es el umbral de peso para considerar una categoría relacionada.
const relatedMinWeight = 0.5

// relatedRadius es el radio en el que se buscan categorías relacionadas y ofertas.
const relatedRadius = 10000.0

type SearchService struct {
	Repo      *repository.SearchRepository
	OfferRepo *repository.OfferRepository
}

func NewSearchService(repo *repository.SearchRepository, offerRepo *repository.OfferRepository) *SearchService {
	return &SearchService{Repo: repo, OfferRepo: offerRepo}
}

// Search ejecuta la búsqueda por niveles:
//   1. exact: coincidencias en el viewport (categoría o texto).
//   2. nearest_beyond: el más cercano ampliando radio 2→5→10→25 km.
//   3. related: categorías relacionadas cercanas (máx 3).
//   4. offers: relámpagos activos de categorías relacionadas.
// Si no hay nada realmente relacionado, las listas quedan vacías (sin relleno).
func (s *SearchService) Search(q string, minLat, maxLat, minLng, maxLng, lat, lng float64) (*dtos.SearchResponse, error) {
	res := &dtos.SearchResponse{
		Exact:   []dtos.SearchBusiness{},
		Related: []dtos.SearchBusiness{},
		Offers:  []dtos.RelampagoListItem{},
	}

	hasBBox := minLat != 0 || maxLat != 0 || minLng != 0 || maxLng != 0

	category, err := s.Repo.MatchCategory(q)
	if err != nil {
		return nil, err
	}

	// Nivel 1: coincidencias en el viewport.
	var exact []models.Entity
	if hasBBox {
		if category != nil {
			exact, _ = s.Repo.FindByCategoryInBBox(category.ID, minLat, maxLat, minLng, maxLng, 50)
		} else {
			exact, _ = s.Repo.FindByTextInBBox(q, minLat, maxLat, minLng, maxLng, 50)
		}
	}

	if len(exact) > 0 {
		res.Exact = s.toBusinesses(exact, lat, lng)
		res.Count = len(exact)
		s.fillRelatedAndOffers(res, category, lat, lng)
		return res, nil
	}

	// Nivel 2: el más cercano fuera del viewport.
	if category != nil && (lat != 0 || lng != 0) {
		for _, radius := range radiiMeters {
			blat, bmaxLat, blng, bmaxLng := utils.BBoxAround(lat, lng, radius)
			candidates, _ := s.Repo.FindByCategoryInBBox(category.ID, blat, bmaxLat, blng, bmaxLng, 50)
			nearest, dist := nearestTo(candidates, lat, lng)
			if nearest != nil {
				res.NearestBeyond = &dtos.NearestBusiness{
					Business:   s.toBusiness(nearest, lat, lng, nil),
					DistanceKM: dist / 1000,
				}
				res.Count = 1
				s.fillRelatedAndOffers(res, category, lat, lng)
				return res, nil
			}
		}
	}

	// Niveles 3 y 4: relacionados y ofertas (listas vacías si no hay nada).
	s.fillRelatedAndOffers(res, category, lat, lng)
	return res, nil
}

// fillRelatedAndOffers rellena related (máx 3) y offers (máx 3) para la
// categoría dada, en un radio alrededor del usuario.
func (s *SearchService) fillRelatedAndOffers(res *dtos.SearchResponse, category *models.Category, lat, lng float64) {
	if category == nil || (lat == 0 && lng == 0) {
		return
	}
	relatedCats, err := s.Repo.FindRelatedCategories(category.ID, relatedMinWeight)
	if err != nil || len(relatedCats) == 0 {
		return
	}
	ids := make([]uuid.UUID, 0, len(relatedCats))
	for i := range relatedCats {
		ids = append(ids, relatedCats[i].ID)
	}

	blat, bmaxLat, blng, bmaxLng := utils.BBoxAround(lat, lng, relatedRadius)

	related, _ := s.Repo.FindByCategoriesInBBox(ids, blat, bmaxLat, blng, bmaxLng, 3)
	res.Related = s.toBusinesses(related, lat, lng)

	offers, _ := s.Repo.FindActiveOffersByCategories(ids, blat, bmaxLat, blng, bmaxLng, utils.NowInCartagena(), 3)
	res.Offers = s.toOffers(offers, lat, lng)
}

// Chips devuelve las categorías que existen en el área con su conteo,
// ordenadas por contexto (hora del día) y popularidad del área.
func (s *SearchService) Chips(minLat, maxLat, minLng, maxLng float64, hour int) ([]dtos.CategoryChip, error) {
	rows, err := s.Repo.CountByCategoryInBBox(minLat, maxLat, minLng, maxLng)
	if err != nil {
		return nil, err
	}

	categoryIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		categoryIDs = append(categoryIDs, r.CategoryID)
	}
	var cats []models.Category
	if len(categoryIDs) > 0 {
		if err := s.Repo.DB.Where("id IN ?", categoryIDs).Find(&cats).Error; err != nil {
			return nil, err
		}
	}
	catByID := make(map[uuid.UUID]models.Category, len(cats))
	for _, c := range cats {
		catByID[c.ID] = c
	}

	type chip struct {
		cat   models.Category
		count int64
		score float64
	}
	chips := make([]chip, 0, len(rows))
	for _, r := range rows {
		c := catByID[r.CategoryID]
		chips = append(chips, chip{cat: c, count: r.Count, score: float64(r.Count) + hourBoost(c, hour)})
	}
	sort.SliceStable(chips, func(i, j int) bool { return chips[i].score > chips[j].score })

	out := make([]dtos.CategoryChip, 0, len(chips))
	for _, c := range chips {
		out = append(out, dtos.CategoryChip{
			ID:    c.cat.ID,
			Name:  c.cat.Name,
			Icon:  c.cat.Icon,
			Color: c.cat.Color,
			Count: c.count,
		})
	}
	return out, nil
}

// hourBoost da un pequeño empujón contextual por hora del día: mañana y
// mediodía la "calidez" (café/comida) sube; noche, bares también.
func hourBoost(c models.Category, hour int) float64 {
	if c.Family != "calidez" {
		return 0
	}
	switch {
	case hour >= 6 && hour < 12:
		return 0.5
	case hour >= 11 && hour < 16:
		return 0.3
	case hour >= 18 && hour < 24:
		return 0.4
	default:
		return 0
	}
}

func nearestTo(entities []models.Entity, lat, lng float64) (*models.Entity, float64) {
	var best *models.Entity
	bestDist := math.MaxFloat64
	for i := range entities {
		e := &entities[i]
		d := utils.DistanceMeters(lat, lng, e.Latitude, e.Longitude)
		if d < bestDist {
			bestDist = d
			best = e
		}
	}
	if best == nil {
		return nil, 0
	}
	return best, bestDist
}

func (s *SearchService) toBusinesses(entities []models.Entity, lat, lng float64) []dtos.SearchBusiness {
	ids := make([]uuid.UUID, len(entities))
	for i := range entities {
		ids[i] = entities[i].ID
	}
	offers, _ := s.OfferRepo.FindActiveForEntities(ids, utils.NowInCartagena())

	out := make([]dtos.SearchBusiness, 0, len(entities))
	for i := range entities {
		out = append(out, s.toBusiness(&entities[i], lat, lng, offers))
	}
	return out
}

func (s *SearchService) toBusiness(e *models.Entity, lat, lng float64, offers map[uuid.UUID]*models.Offer) dtos.SearchBusiness {
	dist := 0.0
	if lat != 0 || lng != 0 {
		dist = utils.DistanceMeters(lat, lng, e.Latitude, e.Longitude) / 1000
	}
	hasOffer := false
	if offers != nil {
		hasOffer = offers[e.ID] != nil
	}
	return dtos.SearchBusiness{
		ID:             e.ID,
		Name:           e.Name,
		CategoryID:     e.Category.ID,
		CategoryName:   e.Category.Name,
		CategoryIcon:   e.Category.Icon,
		CategoryColor:  e.Category.Color,
		ProfileURL:     e.ProfileURL,
		Latitude:       e.Latitude,
		Longitude:      e.Longitude,
		IsOpenNow:      utils.IsOpenNow(e.Hours, utils.NowInCartagena()),
		HasHours:       len(e.Hours) > 0,
		HasActiveOffer: hasOffer,
		DistanceKM:     dist,
	}
}

func (s *SearchService) toOffers(offers []models.Offer, lat, lng float64) []dtos.RelampagoListItem {
	out := make([]dtos.RelampagoListItem, 0, len(offers))
	for i := range offers {
		o := &offers[i]
		dist := 0.0
		if lat != 0 || lng != 0 {
			dist = utils.DistanceMeters(lat, lng, o.Entity.Latitude, o.Entity.Longitude)
		}
		out = append(out, dtos.RelampagoListItem{
			OfferResponse:  dtos.OfferResponseFrom(o),
			DistanceMeters: dist,
			EntityName:     o.Entity.Name,
			CategoryName:   o.Entity.Category.Name,
		})
	}
	return out
}
