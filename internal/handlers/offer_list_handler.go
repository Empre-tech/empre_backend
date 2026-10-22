package handlers

import (
	"net/http"
	"strconv"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"
	"empre_backend/pkg/utils"

	"github.com/gin-gonic/gin"
)

type OfferListHandler struct {
	Service *services.OfferListService
}

func NewOfferListHandler(service *services.OfferListService) *OfferListHandler {
	return &OfferListHandler{Service: service}
}

// List devuelve los relámpagos y promociones activos para la pestaña Ofertas,
// ordenados por "near" o "trending". Público.
// @Router /api/offers [get]
func (h *OfferListHandler) List(c *gin.Context) {
	lat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	long, _ := strconv.ParseFloat(c.Query("long"), 64)
	categoryID := c.Query("category")
	order := c.DefaultQuery("order", "near") // "near" | "trending"

	offers, promotions, err := h.Service.List(lat, long, categoryID, order)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	relampagos := make([]dtos.RelampagoListItem, 0, len(offers))
	for i := range offers {
		o := &offers[i]
		relampagos = append(relampagos, dtos.RelampagoListItem{
			OfferResponse:  dtos.OfferResponseFrom(o),
			DistanceMeters: utils.DistanceMeters(lat, long, o.Entity.Latitude, o.Entity.Longitude),
			EntityName:     o.Entity.Name,
			CategoryName:   o.Entity.Category.Name,
		})
	}

	promos := make([]dtos.PromotionListItem, 0, len(promotions))
	for i := range promotions {
		p := &promotions[i]
		promos = append(promos, dtos.PromotionListItem{
			PromotionResponse: dtos.PromotionResponseFrom(p),
			DistanceMeters:    utils.DistanceMeters(lat, long, p.Entity.Latitude, p.Entity.Longitude),
			EntityName:        p.Entity.Name,
			CategoryName:      p.Entity.Category.Name,
		})
	}

	c.JSON(http.StatusOK, gin.H{"relampagos": relampagos, "promotions": promos})
}
