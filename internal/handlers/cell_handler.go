package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type CellHandler struct {
	Service *services.CellService
}

func NewCellHandler(service *services.CellService) *CellHandler {
	return &CellHandler{Service: service}
}

// List devuelve los negocios del rectángulo visible agrupados en casillas (conteo, oferta
// vigente y categoría dominante). Público. `lat_delta` es lo que muestra el mapa a lo alto, en
// grados, y decide el tamaño de casilla.
// @Router /api/discovery/cells [get]
func (h *CellHandler) List(c *gin.Context) {
	minLat, _ := strconv.ParseFloat(c.Query("min_lat"), 64)
	maxLat, _ := strconv.ParseFloat(c.Query("max_lat"), 64)
	minLng, _ := strconv.ParseFloat(c.Query("min_lng"), 64)
	maxLng, _ := strconv.ParseFloat(c.Query("max_lng"), 64)
	latDelta, _ := strconv.ParseFloat(c.Query("lat_delta"), 64)

	cells, size, truncated, err := h.Service.Cells(services.CellQuery{
		MinLat: minLat, MaxLat: maxLat, MinLng: minLng, MaxLng: maxLng,
		LatDelta:      latDelta,
		CategoryID:    c.Query("category"),
		SubcategoryID: c.Query("subcategory"),
	})
	if errors.Is(err, services.ErrCellsBox) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudieron agrupar los negocios"})
		return
	}
	total := 0
	for _, cell := range cells {
		total += cell.Count
	}
	if cells == nil {
		cells = []services.Cell{}
	}
	c.JSON(http.StatusOK, gin.H{"items": cells, "cell_deg": size, "total": total, "truncated": truncated})
}
