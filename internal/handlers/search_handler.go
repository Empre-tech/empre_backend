package handlers

import (
	"net/http"
	"strconv"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type SearchHandler struct {
	Service *services.SearchService
}

func NewSearchHandler(service *services.SearchService) *SearchHandler {
	return &SearchHandler{Service: service}
}

// Search responde con la búsqueda por niveles (exact → nearest_beyond →
// related → offers). Público.
// @Router /api/search [get]
func (h *SearchHandler) Search(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Falta el término de búsqueda"})
		return
	}

	minLat, _ := strconv.ParseFloat(c.Query("min_lat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("max_lat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("min_lng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("max_lng"), 64)
	lat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	lng, _ := strconv.ParseFloat(c.Query("long"), 64)

	res, err := h.Service.Search(q, minLat, maxLat, minLng, maxLng, lat, lng)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

// Chips devuelve las categorías que existen en el área con su conteo,
// ordenadas por contexto (hora del día). Público.
// @Router /api/categories/chips [get]
func (h *SearchHandler) Chips(c *gin.Context) {
	minLat, _ := strconv.ParseFloat(c.Query("min_lat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("max_lat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("min_lng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("max_lng"), 64)
	hour, _ := strconv.Atoi(c.DefaultQuery("hour", ""))

	chips, err := h.Service.Chips(minLat, maxLat, minLng, maxLng, hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if chips == nil {
		chips = []dtos.CategoryChip{}
	}

	c.JSON(http.StatusOK, gin.H{"data": chips})
}
