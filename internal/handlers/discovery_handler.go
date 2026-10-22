package handlers

import (
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"

	"github.com/gin-gonic/gin"
)

type DiscoveryHandler struct {
	Service *services.RankingService
}

func NewDiscoveryHandler(service *services.RankingService) *DiscoveryHandler {
	return &DiscoveryHandler{Service: service}
}

// Discover returns the N best businesses for the visible rectangle, ranked
// and diversified, plus the total count. Public endpoint.
// @Router /api/discovery [get]
func (h *DiscoveryHandler) Discover(c *gin.Context) {
	minLat, _ := strconv.ParseFloat(c.Query("min_lat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("max_lat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("min_lng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("max_lng"), 64)
	// Ubicación del usuario (referencia para la distancia); 0/0 = sin señal.
	refLat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	refLng, _ := strconv.ParseFloat(c.Query("long"), 64)
	categoryID := c.Query("category")
	subcategoryID := c.Query("subcategory")
	query := c.Query("q")
	offerFilter := c.Query("offer") // "", "now", "today", "week"
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "15"))

	items, total, err := h.Service.Discover(minLat, maxLat, minLng, maxLng, refLat, refLng, categoryID, subcategoryID, query, offerFilter, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := make([]dtos.DiscoveryDTO, 0, len(items))
	for i := range items {
		it := items[i]
		e := it.Entity

		var active *dtos.OfferResponse
		if it.ActiveOffer != nil {
			o := dtos.OfferResponseFrom(it.ActiveOffer)
			active = &o
		}

		closesAt, nextOpen := openingInfo(e.Hours, it.IsOpenNow, utils.NowInCartagena())
		photos, photosCount := services.PhotoSample(&e, services.PhotoSampleMax)
		resp = append(resp, dtos.DiscoveryDTO{
			EntityMapDTO: dtos.EntityMapDTO{
				ID:            e.ID,
				Name:          e.Name,
				CategoryID:    e.Category.ID,
				CategoryName:  e.Category.Name,
				CategoryIcon:  e.Category.Icon,
				CategoryColor: e.Category.Color,
				ProfileURL:    services.ProfilePath(&e),
				Latitude:      e.Latitude,
				Longitude:     e.Longitude,
				IsVerified:    e.IsVerified,
				AvgRating:     it.AvgRating,
				ReviewCount:   it.ReviewCount,
				ServiceMode:   e.ServiceMode,
				IsOpenNow:     it.IsOpenNow,
				HasHours:      len(e.Hours) > 0,
			},
			SubcategoryIDs: subcategoryIDs(e.Subcategories),
			Subcategory:    firstSubcategory(e.Subcategories),
			Tagline:        e.Tagline,
			DistanceMeters: it.DistanceMeters,
			HasActiveOffer: it.ActiveOffer != nil,
			ActiveOffer:    active,
			ClosesAt:       closesAt,
			NextOpen:       nextOpen,
			Photos:         photos,
			PhotosCount:    photosCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": resp, "total": total})
}

// Sections returns «Descubre» for the visible rectangle: a few sections built
// on the server (offers ending soon, open now, new posts), each card with one
// reason. Shown when the user hasn't searched. Public endpoint.
// @Router /api/discovery/sections [get]
func (h *DiscoveryHandler) Sections(c *gin.Context) {
	minLat, _ := strconv.ParseFloat(c.Query("min_lat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("max_lat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("min_lng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("max_lng"), 64)
	refLat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	refLng, _ := strconv.ParseFloat(c.Query("long"), 64)

	// «Ver más» de una sección: ?section=offers&limit=50 devuelve solo esa, con más negocios.
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(services.SectionLimit)))
	if limit <= 0 || limit > 50 {
		limit = services.SectionLimit
	}
	sections, err := h.Service.SectionsN(minLat, maxLat, minLng, maxLng, refLat, refLng, c.Query("category"), c.Query("subcategory"), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if only := c.Query("section"); only != "" {
		filtered := sections[:0]
		for _, sec := range sections {
			if sec.ID == only {
				filtered = append(filtered, sec)
			}
		}
		sections = filtered
	}
	c.JSON(http.StatusOK, gin.H{"sections": SectionsToDTO(sections)})
}

// SectionsToDTO convierte las secciones del servicio a la respuesta pública.
func SectionsToDTO(sections []services.Section) []dtos.DiscoverSectionDTO {
	out := make([]dtos.DiscoverSectionDTO, 0, len(sections))
	for _, sec := range sections {
		items := make([]dtos.DiscoverItemDTO, 0, len(sec.Items))
		for _, it := range sec.Items {
			e := &it.Candidate.Entity
			sub := ""
			if len(e.Subcategories) > 0 {
				sub = e.Subcategories[0].Name
			}
			photos, photosCount := services.PhotoSample(e, services.PhotoSampleMax)
			items = append(items, dtos.DiscoverItemDTO{
				EntityID:       e.ID,
				Name:           e.Name,
				CategoryName:   e.Category.Name,
				CategoryIcon:   e.Category.Icon,
				CategoryColor:  e.Category.Color,
				Subcategory:    sub,
				Latitude:       e.Latitude,
				Longitude:      e.Longitude,
				DistanceMeters: it.Candidate.DistanceMeters,
				IsOpenNow:      it.Candidate.IsOpenNow,
				ClosesAt:       it.Candidate.ClosesAt,
				CoverURL:       services.CoverURL(e),
				ProfileURL:     services.ProfilePath(e),
				Tagline:        e.Tagline,
				Reason:         dtos.DiscoverReasonDTO{Kind: it.Reason.Kind, EndsAt: it.Reason.EndsAt, PostedAt: it.Reason.PostedAt, OfferTitle: it.Reason.OfferTitle},
				Photos:         photos,
				PhotosCount:    photosCount,
			})
		}
		out = append(out, dtos.DiscoverSectionDTO{ID: sec.ID, Title: sec.Title, Items: items})
	}
	return out
}

func subcategoryIDs(subs []models.Subcategory) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(subs))
	for _, s := range subs {
		ids = append(ids, s.ID)
	}
	return ids
}

func firstSubcategory(subs []models.Subcategory) string {
	if len(subs) == 0 {
		return ""
	}
	return subs[0].Name
}

// openingInfo da lo que la ficha del mapa necesita del horario: a qué hora cierra (si está abierto) o cuándo vuelve
// a abrir (si está cerrado). Sin horario no dice nada.
func openingInfo(hours []models.BusinessHour, open bool, now time.Time) (string, *dtos.NextOpenDTO) {
	if len(hours) == 0 {
		return "", nil
	}
	if open {
		return utils.ClosesAt(hours, now), nil
	}
	if n, ok := utils.NextOpening(hours, now); ok {
		return "", &dtos.NextOpenDTO{DaysAhead: n.DaysAhead, Weekday: n.Weekday, Time: n.Time}
	}
	return "", nil
}
