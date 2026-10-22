package handlers

import (
	"net/http"
	"time"

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

// ZoneActivityHandler: el brillo de las zonas y los pulsos del mapa vivo (3.41).
type ZoneActivityHandler struct{ service *services.ZoneActivityService }

func NewZoneActivityHandler(s *services.ZoneActivityService) *ZoneActivityHandler {
	return &ZoneActivityHandler{service: s}
}

type zoneActivityDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	RadiusM    int     `json:"radius_m"`
	Businesses int     `json:"businesses"`
	HasOffers  bool    `json:"has_offers"`
	Growing    bool    `json:"growing"`
}

type freshOfferDTO struct {
	OfferID     string    `json:"offer_id"`
	EntityID    string    `json:"entity_id"`
	Latitude    float64   `json:"latitude"`
	Longitude   float64   `json:"longitude"`
	PublishedAt time.Time `json:"published_at"`
}

// Activity godoc
// @Summary  Actividad real por zona y ofertas recién publicadas, para el mapa vivo (3.41)
// @Tags     zones
// @Produce  json
// @Param    lat query number false "Latitud (decide la ciudad)"
// @Param    long query number false "Longitud"
// @Param    city query string false "Última ciudad conocida (solo sin ubicación)"
// @Router   /zones/activity [get]
func (h *ZoneActivityHandler) Activity(c *gin.Context) {
	lat, long, hasLoc := marketPlace(c)
	city, act, fresh, err := h.service.For(lat, long, hasLoc, c.Query("city"), time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudo cargar la actividad de las zonas"})
		return
	}
	zones := make([]zoneActivityDTO, 0, len(act))
	for _, a := range act {
		zones = append(zones, zoneActivityDTO{ID: a.Zone.ID.String(), Name: a.Zone.Name, Latitude: a.Zone.Latitude, Longitude: a.Zone.Longitude, RadiusM: a.Zone.RadiusM, Businesses: a.Businesses, HasOffers: a.HasOffers, Growing: a.Growing})
	}
	offers := make([]freshOfferDTO, 0, len(fresh))
	for _, f := range fresh {
		offers = append(offers, freshOfferDTO{OfferID: f.OfferID.String(), EntityID: f.EntityID.String(), Latitude: f.Latitude, Longitude: f.Longitude, PublishedAt: f.PublishedAt})
	}
	c.JSON(http.StatusOK, gin.H{"city": city, "zones": zones, "fresh_offers": offers})
}
