package services

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Ofertas como marketplace (3.38): junta las señales reales y arma las secciones con BuildMarket. Pocas consultas
// agrupadas (ofertas, búsquedas, reclamos, vistas, valoraciones y, con sesión, seguidos y reclamos propios), sin
// importar cuántas ofertas haya.

const (
	MarketTermsWindow   = 7 * 24 * time.Hour
	MarketTermsMin      = 5 // un término se muestra solo si se buscó al menos 5 veces (privacidad y relevancia)
	MarketTermsMax      = 6
	MarketTrendWindow   = 48 * time.Hour
	marketViewsPerClaim = 4.0 // 4 vistas pesan como un reclamo
)

type MarketTerm struct {
	Term  string `json:"term"`
	Label string `json:"label"`
}

type MarketResult struct {
	Sections   MarketSections
	Terms      []MarketTerm
	Term       string
	HasLoc     bool
	Lat, Long  float64
	Personal   bool
	Categories map[uuid.UUID]bool
	City       string       // ciudad de la vitrina; vacía = sin ciudad (se ve todo)
	Here       *models.Zone // zona donde está la persona
	Zones      []MarketZone
	Featured   *MarketCandidate // «Mayor ahorro de hoy» (3.40); nil si ninguna termina hoy con ahorro
}

type OfferMarketService struct {
	db      *gorm.DB
	reviews *ReviewService
}

func NewOfferMarketService(db *gorm.DB, reviews *ReviewService) *OfferMarketService {
	return &OfferMarketService{db: db, reviews: reviews}
}

func termLabel(t string) string {
	r := []rune(t)
	if len(r) == 0 {
		return t
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// TopSearchTerms: los términos más buscados de la semana (sin usuario: los eventos son anónimos), con un mínimo.
func (s *OfferMarketService) TopSearchTerms(now time.Time) ([]string, error) {
	type row struct {
		Q string
		N int
	}
	var rows []row
	err := s.db.Raw(`
		SELECT lower(trim(props->>'query')) AS q, count(*) AS n
		FROM events
		WHERE name = 'search_submitted' AND created_at > ? AND coalesce(props->>'query', '') <> '' AND props->>'query' <> ?
		GROUP BY 1 HAVING count(*) >= ?
		ORDER BY n DESC, q ASC
		LIMIT 20`, now.Add(-MarketTermsWindow), omittedQuery, MarketTermsMin).Scan(&rows).Error
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Q)
	}
	return out, err
}

func (s *OfferMarketService) trendScores(now time.Time) (map[uuid.UUID]float64, error) {
	type row struct {
		ID uuid.UUID
		N  float64
	}
	out := map[uuid.UUID]float64{}
	var claims []row
	if err := s.db.Raw(`SELECT offer_id AS id, count(*) AS n FROM offer_claims WHERE claimed_at > ? GROUP BY offer_id`, now.Add(-MarketTrendWindow)).Scan(&claims).Error; err != nil {
		return out, err
	}
	for _, r := range claims {
		out[r.ID] += r.N
	}
	var views []row
	if err := s.db.Raw(`SELECT (props->>'offer_id')::uuid AS id, count(*) AS n FROM events
		WHERE name = 'offer_viewed' AND created_at > ? AND props->>'offer_id' ~ '^[0-9a-fA-F-]{36}$'
		GROUP BY 1`, now.Add(-MarketTrendWindow)).Scan(&views).Error; err != nil {
		return out, err
	}
	for _, r := range views {
		out[r.ID] += r.N / marketViewsPerClaim
	}
	return out, nil
}

// personalReasons: por qué una oferta es para esta persona. Seguir el negocio pesa más que haber reclamado algo de su
// categoría. Solo se usan datos de la propia persona.
func (s *OfferMarketService) personalReasons(viewer uuid.UUID, offers []models.Offer) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	var followed []uuid.UUID
	if err := s.db.Model(&models.Favorite{}).Where("user_id = ?", viewer).Pluck("entity_id", &followed).Error; err != nil {
		return out, err
	}
	var claimedCats []uuid.UUID
	if err := s.db.Raw(`SELECT DISTINCT e.category_id FROM offer_claims c JOIN entities e ON e.id = c.entity_id WHERE c.user_id = ?`, viewer).Scan(&claimedCats).Error; err != nil {
		return out, err
	}
	f := map[uuid.UUID]bool{}
	for _, id := range followed {
		f[id] = true
	}
	cc := map[uuid.UUID]bool{}
	for _, id := range claimedCats {
		cc[id] = true
	}
	for _, o := range offers {
		switch {
		case f[o.EntityID]:
			out[o.ID] = "Sigues a " + o.Entity.Name
		case cc[o.Entity.CategoryID]:
			out[o.ID] = "Porque reclamaste en " + o.Entity.Category.Name
		}
	}
	return out, nil
}

// marketBase: las ofertas vigentes (con el filtro de categoría) como candidatas, la ciudad de la vitrina y las zonas.
type marketBase struct {
	cands []MarketCandidate // ya de la ciudad
	city  string
	here  *models.Zone
	zones []models.Zone
}

func (s *OfferMarketService) load(viewer *uuid.UUID, lat, long float64, hasLoc bool, cityHint, categoryRaw string, withSignals bool, now time.Time) (marketBase, error) {
	var b marketBase
	var offers []models.Offer
	q := s.db.Preload("Entity.Category").Preload("Items").
		Where("ends_at > ? AND starts_at <= ? AND used_slots < total_slots", now, now)
	if ids, asked := repository.ParseCategoryIDs(categoryRaw); asked {
		if len(ids) == 0 {
			return b, nil
		}
		q = q.Where("entity_id IN (SELECT id FROM entities WHERE category_id IN ? AND deleted_at IS NULL)", ids)
	}
	if err := q.Order("ends_at ASC").Find(&offers).Error; err != nil {
		return b, err
	}
	zones, err := NewZoneService(s.db).List("")
	if err != nil {
		return b, err
	}
	b.zones = zones
	// La ciudad sale de todas las ofertas vigentes (sin el filtro de categoría no cambia: se resuelve con estas mismas).
	points := make([]CityPoint, 0, len(offers))
	for _, o := range offers {
		points = append(points, CityPoint{City: o.Entity.City, Lat: o.Entity.Latitude, Long: o.Entity.Longitude})
	}
	b.city, b.here = ResolveCity(zones, points, lat, long, hasLoc, cityHint)

	inCity := offers[:0:0]
	for _, o := range offers {
		if b.city == "" || SameCity(o.Entity.City, b.city) {
			inCity = append(inCity, o)
		}
	}
	trend := map[uuid.UUID]float64{}
	ratings := map[uuid.UUID]repository.ReviewSummary{}
	reasons := map[uuid.UUID]string{}
	if withSignals {
		if trend, err = s.trendScores(now); err != nil {
			return b, err
		}
		entityIDs := make([]uuid.UUID, 0, len(inCity))
		for _, o := range inCity {
			entityIDs = append(entityIDs, o.EntityID)
		}
		if s.reviews != nil && len(entityIDs) > 0 {
			if r, err := s.reviews.Summaries(entityIDs); err == nil {
				ratings = r
			}
		}
		if viewer != nil {
			if reasons, err = s.personalReasons(*viewer, inCity); err != nil {
				return b, err
			}
		}
	}
	b.cands = make([]MarketCandidate, 0, len(inCity))
	for _, o := range inCity {
		d := -1.0
		if hasLoc {
			d = utils.DistanceMeters(lat, long, o.Entity.Latitude, o.Entity.Longitude)
		}
		b.cands = append(b.cands, MarketCandidate{Offer: o, DistanceM: d, Trend: trend[o.ID], Rating: ratings[o.EntityID].Avg, Reason: reasons[o.ID]})
	}
	return b, nil
}

// Market arma la vitrina para quien mira (viewer nil = sin sesión). `cityHint` es la última ciudad conocida; solo se usa
// sin ubicación.
func (s *OfferMarketService) Market(viewer *uuid.UUID, lat, long float64, hasLoc bool, cityHint, categoryRaw, term string, now time.Time) (MarketResult, error) {
	res := MarketResult{HasLoc: hasLoc, Lat: lat, Long: long, Personal: viewer != nil}
	b, err := s.load(viewer, lat, long, hasLoc, cityHint, categoryRaw, true, now)
	if err != nil {
		return res, err
	}
	res.City, res.Here = b.city, b.here
	res.Zones = BuildZones(b.zones, b.cands, b.city, b.here, lat, long, hasLoc)
	cands := b.cands

	// Términos: los más buscados, primero los que hoy tienen alguna oferta en la ciudad.
	raw, err := s.TopSearchTerms(now)
	if err != nil {
		return res, err
	}
	type scored struct {
		t   string
		hit bool
	}
	terms := []scored{}
	for _, t := range raw {
		hit := false
		for _, c := range cands {
			if MatchesTerm(c.Offer, t) {
				hit = true
				break
			}
		}
		terms = append(terms, scored{t, hit})
	}
	sort.SliceStable(terms, func(i, j int) bool { return terms[i].hit && !terms[j].hit })
	if len(terms) > MarketTermsMax {
		terms = terms[:MarketTermsMax]
	}
	chosen := ""
	for _, t := range terms {
		res.Terms = append(res.Terms, MarketTerm{Term: t.t, Label: termLabel(t.t)})
		if strings.EqualFold(t.t, strings.TrimSpace(term)) {
			chosen = t.t
		}
	}
	if chosen == "" && len(terms) > 0 {
		chosen = terms[0].t
	}
	res.Term = chosen

	// La destacada sale primero y no se repite en las secciones (sí en la lista completa).
	if f := PickFeatured(cands, now); f != nil {
		res.Featured = f
		cands = WithoutOffer(cands, f.Offer.ID)
	}
	res.Sections = BuildMarket(cands, MarketOptions{HasLocation: hasLoc, Term: chosen, Personal: viewer != nil})
	return res, nil
}

// MarketAllResult es una tanda de «Todas las ofertas de la ciudad».
type MarketAllResult struct {
	City  string
	Items []MarketCandidate
	Next  int // -1 = no hay más
}

// MarketAll: la lista completa de la ciudad, por tandas, con zona, búsqueda y orden. Resuelve la ciudad igual que
// Market, así la lista y las secciones siempre hablan de la misma.
func (s *OfferMarketService) MarketAll(lat, long float64, hasLoc bool, cityHint, categoryRaw string, zone *uuid.UUID, query, sortKey string, offset int, now time.Time) (MarketAllResult, error) {
	b, err := s.load(nil, lat, long, hasLoc, cityHint, categoryRaw, true, now)
	if err != nil {
		return MarketAllResult{Next: -1}, err
	}
	page, next := PageAll(b.cands, AllOptions{HasLocation: hasLoc, Zone: zone, Query: query, Sort: sortKey}, offset, MarketPageSize)
	return MarketAllResult{City: b.city, Items: page, Next: next}, nil
}
