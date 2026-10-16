package handlers

import (
	"net/http"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type ZoneHandler struct {
	Service *services.ZoneService
}

func NewZoneHandler(service *services.ZoneService) *ZoneHandler {
	return &ZoneHandler{Service: service}
}

type zoneDTO struct {
	ID        string  `json:"id"`
	City      string  `json:"city"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	RadiusM   int     `json:"radius_m"`
}

// List devuelve las zonas activas de una ciudad (`?city=`); una ciudad sin zonas devuelve una
// lista vacía, nunca un error. Público: la app las descarga una vez y las guarda.
// @Router /api/zones [get]
func (h *ZoneHandler) List(c *gin.Context) {
	zones, err := h.Service.List(c.Query("city"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudieron cargar las zonas"})
		return
	}
	items := make([]zoneDTO, 0, len(zones))
	for _, z := range zones {
		items = append(items, zoneDTO{ID: z.ID.String(), City: z.City, Name: z.Name, Latitude: z.Latitude, Longitude: z.Longitude, RadiusM: z.RadiusM})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
