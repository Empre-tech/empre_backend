package handlers

import (
	"net/http"
	"strconv"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type DiscoveryHandler struct {
	Service *services.RankingService
}

func NewDiscoveryHandler(service *services.RankingService) *DiscoveryHandler {
	return &DiscoveryHandler{Service: service}
}

// Discover returns the N best businesses for the visible area, ranked and
// diversified. Public endpoint.
// @Router /api/discovery [get]
func (h *DiscoveryHandler) Discover(c *gin.Context) {
	lat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	long, _ := strconv.ParseFloat(c.Query("long"), 64)
	radius, _ := strconv.ParseFloat(c.Query("radius"), 64)
	categoryID := c.Query("category")
	subcategoryID := c.Query("subcategory")
	query := c.Query("q")
	offerFilter := c.Query("offer") // "", "now", "today", "week"
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "15"))

	items, err := h.Service.Discover(lat, long, radius, categoryID, subcategoryID, query, offerFilter, limit)
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

		resp = append(resp, dtos.DiscoveryDTO{
			EntityMapDTO: dtos.EntityMapDTO{
				ID:            e.ID,
				Name:          e.Name,
				CategoryID:    e.Category.ID,
				CategoryName:  e.Category.Name,
				CategoryIcon:  e.Category.Icon,
				CategoryColor: e.Category.Color,
				ProfileURL:    e.ProfileURL,
				Latitude:      e.Latitude,
				Longitude:     e.Longitude,
				IsVerified:    e.IsVerified,
				AvgRating:     it.AvgRating,
				ReviewCount:   it.ReviewCount,
				ServiceMode:   e.ServiceMode,
				IsOpenNow:     it.IsOpenNow,
				HasHours:      len(e.Hours) > 0,
			},
			DistanceMeters: it.DistanceMeters,
			HasActiveOffer: it.ActiveOffer != nil,
			ActiveOffer:    active,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": resp})
}
