package services

import (
	"sort"
	"strings"
	"time"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// «Descubre»: lo que el panel del mapa muestra cuando la persona todavía no
// buscó nada. El servidor arma las secciones (la app solo las dibuja), así las
// reglas viven en un solo lugar y pueden cambiar sin publicar otra versión.

const (
	// SectionLimit es cuántos negocios trae cada sección como máximo.
	SectionLimit = 6
	// SectionMin: una sección con menos negocios no se envía (se vería vacía).
	SectionMin = 3
	// NewWindow es cuánto tiempo cuenta una publicación como «lo nuevo».
	NewWindow = 7 * 24 * time.Hour
)

// Identificadores y títulos de las secciones, en el orden en que se muestran.
const (
	SectionOffers = "offers"
	SectionOpen   = "open"
	SectionNew    = "new"
)

var sectionTitles = map[string]string{
	SectionOffers: "Ofertas que terminan pronto",
	SectionOpen:   "Abiertos ahora, cerca de ti",
	SectionNew:    "Lo nuevo",
}

// SectionCandidate es un negocio del área con las señales que deciden en qué
// sección entra.
type SectionCandidate struct {
	Entity         models.Entity
	DistanceMeters float64
	IsOpenNow      bool
	ClosesAt       string
	Offer          *models.Offer // la oferta vigente que termina antes
	LatestPost     *time.Time    // cuándo publicó por última vez
}

// SectionReason es la única razón que muestra la tarjeta.
type SectionReason struct {
	Kind       string // offer | open | new
	EndsAt     *time.Time
	PostedAt   *time.Time
	OfferTitle string
}

type SectionItem struct {
	Candidate *SectionCandidate
	Reason    SectionReason
}

type Section struct {
	ID    string
	Title string
	Items []SectionItem
}

// BuildSections arma las secciones a partir de los candidatos. Es pura: no toca
// la base de datos, para poder probar las reglas sin ella.
func BuildSections(cands []SectionCandidate, now time.Time) []Section {
	return BuildSectionsN(cands, now, SectionLimit)
}

// BuildSectionsN es BuildSections con otro tope por sección (para «Ver más»).
func BuildSectionsN(cands []SectionCandidate, now time.Time, limit int) []Section {
	pick := func(id string, keep func(*SectionCandidate) bool, less func(a, b *SectionCandidate) bool, reason func(*SectionCandidate) SectionReason) Section {
		var list []*SectionCandidate
		for i := range cands {
			if keep(&cands[i]) {
				list = append(list, &cands[i])
			}
		}
		sort.SliceStable(list, func(i, j int) bool { return less(list[i], list[j]) })
		if len(list) > limit {
			list = list[:limit]
		}
		items := make([]SectionItem, 0, len(list))
		for _, c := range list {
			items = append(items, SectionItem{Candidate: c, Reason: reason(c)})
		}
		return Section{ID: id, Title: sectionTitles[id], Items: items}
	}
	byDistance := func(a, b *SectionCandidate) bool {
		if a.DistanceMeters != b.DistanceMeters {
			return a.DistanceMeters < b.DistanceMeters
		}
		return a.Entity.Name < b.Entity.Name
	}

	all := []Section{
		pick(SectionOffers,
			func(c *SectionCandidate) bool { return c.Offer != nil && c.Offer.EndsAt.After(now) },
			func(a, b *SectionCandidate) bool {
				if !a.Offer.EndsAt.Equal(b.Offer.EndsAt) {
					return a.Offer.EndsAt.Before(b.Offer.EndsAt)
				}
				return byDistance(a, b)
			},
			func(c *SectionCandidate) SectionReason {
				ends := c.Offer.EndsAt
				return SectionReason{Kind: SectionOffers, EndsAt: &ends, OfferTitle: c.Offer.Title}
			}),
		pick(SectionOpen,
			func(c *SectionCandidate) bool { return c.IsOpenNow },
			byDistance,
			func(c *SectionCandidate) SectionReason { return SectionReason{Kind: SectionOpen} }),
		pick(SectionNew,
			func(c *SectionCandidate) bool {
				return c.LatestPost != nil && !c.LatestPost.After(now) && now.Sub(*c.LatestPost) <= NewWindow
			},
			func(a, b *SectionCandidate) bool { return a.LatestPost.After(*b.LatestPost) },
			func(c *SectionCandidate) SectionReason {
				posted := *c.LatestPost
				return SectionReason{Kind: SectionNew, PostedAt: &posted}
			}),
	}

	out := make([]Section, 0, len(all))
	for _, sec := range all {
		if len(sec.Items) >= SectionMin {
			out = append(out, sec)
		}
	}
	return out
}

// CoverURL es la portada de la tarjeta: el banner del negocio o, si no tiene,
// su primera foto (los videos no sirven de portada). Vacío = sin foto.
func CoverURL(e *models.Entity) string {
	if e.BannerURL != "" {
		return e.BannerURL
	}
	photos := append([]models.EntityPhoto(nil), e.Photos...)
	sort.SliceStable(photos, func(i, j int) bool { return photos[i].Order < photos[j].Order })
	for _, p := range photos {
		if p.Media.URL != "" && !strings.HasPrefix(p.Media.ContentType, "video/") {
			return p.Media.URL
		}
	}
	return ""
}

// Sections arma «Descubre» para el rectángulo visible: negocios del área (con
// la categoría elegida), su oferta vigente, si están abiertos y su última
// publicación.
func (s *RankingService) Sections(minLat, maxLat, minLng, maxLng, refLat, refLng float64, categoryID, subcategoryID string) ([]Section, error) {
	return s.SectionsN(minLat, maxLat, minLng, maxLng, refLat, refLng, categoryID, subcategoryID, SectionLimit)
}

// SectionsN es Sections con otro tope por sección.
func (s *RankingService) SectionsN(minLat, maxLat, minLng, maxLng, refLat, refLng float64, categoryID, subcategoryID string, limit int) ([]Section, error) {
	now := utils.NowInCartagena()
	entities, _, err := s.EntityService.FindByBBox(minLat, maxLat, minLng, maxLng, categoryID, subcategoryID, "", 200)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(entities))
	for i := range entities {
		ids[i] = entities[i].ID
	}
	offers, err := s.OfferRepo.FindActiveForEntities(ids, now)
	if err != nil {
		return nil, err
	}
	posts, err := s.EntityService.Repo.LatestPostTimes(ids)
	if err != nil {
		return nil, err
	}

	cands := make([]SectionCandidate, 0, len(entities))
	for i := range entities {
		e := entities[i]
		dist := 0.0
		if refLat != 0 || refLng != 0 {
			dist = utils.DistanceMeters(refLat, refLng, e.Latitude, e.Longitude)
		}
		c := SectionCandidate{Entity: e, DistanceMeters: dist, IsOpenNow: utils.IsOpenNow(e.Hours, now), Offer: offers[e.ID]}
		if c.IsOpenNow {
			c.ClosesAt = utils.ClosesAt(e.Hours, now)
		}
		if t, ok := posts[e.ID]; ok {
			t := t
			c.LatestPost = &t
		}
		cands = append(cands, c)
	}
	return BuildSectionsN(cands, now, limit), nil
}
