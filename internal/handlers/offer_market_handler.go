package handlers

import (
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MarketItem es una oferta en la vitrina: la oferta, su negocio y lo que la tarjeta necesita. Sin conteos de ofertas.
type MarketItem struct {
	dtos.OfferResponse
	EntityName    string `json:"entity_name"`
	CategoryName  string `json:"category_name"`
	CategoryColor string `json:"category_color,omitempty"`
	CategoryIcon  string `json:"category_icon,omitempty"`
	// Photo: portada o foto de perfil del negocio (ruta estable). Vacía = la tarjeta usa el color de la categoría.
	Photo string `json:"photo,omitempty"`
	// DistanceMeters: nulo sin ubicación (nunca una distancia inventada).
	DistanceMeters *float64 `json:"distance_meters"`
	Reason         string   `json:"reason,omitempty"`
	Trending       bool     `json:"trending,omitempty"`
}

type marketSearched struct {
	Terms []services.MarketTerm `json:"terms"`
	Term  string                `json:"term"`
	Items []MarketItem          `json:"items"`
}

type marketForYou struct {
	TopRated bool         `json:"top_rated"`
	Items    []MarketItem `json:"items"`
}

// MarketZoneLook es una categoría del mosaico de una zona.
type MarketZoneLook struct {
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// MarketZoneItem es una tarjeta de «Por zona» (3.39). Sin conteos.
type MarketZoneItem struct {
	ID             uuid.UUID        `json:"id"`
	Name           string           `json:"name"`
	DistanceMeters *float64         `json:"distance_meters"`
	Here           bool             `json:"here"`
	Looks          []MarketZoneLook `json:"looks"`
}

type MarketResponse struct {
	HasLocation bool `json:"has_location"`
	// City: ciudad de la vitrina (de la ubicación o, sin ella, la última conocida). Vacía = todas las ciudades.
	City string `json:"city"`
	// Zone: nombre de la zona donde está la persona; vacío si no está en ninguna.
	Zone     string           `json:"zone"`
	Zones    []MarketZoneItem `json:"zones"`
	Near     []MarketItem     `json:"near"`
	Searched marketSearched   `json:"searched"`
	Trending []MarketItem     `json:"trending"`
	ForYou   marketForYou     `json:"for_you"`
}

// MarketAllResponse es una tanda de «Todas las ofertas de la ciudad». NextCursor vacío = no hay más.
type MarketAllResponse struct {
	City       string       `json:"city"`
	Items      []MarketItem `json:"items"`
	NextCursor string       `json:"next_cursor"`
}

type OfferMarketHandler struct{ service *services.OfferMarketService }

func NewOfferMarketHandler(s *services.OfferMarketService) *OfferMarketHandler {
	return &OfferMarketHandler{service: s}
}

func marketItems(list []services.MarketCandidate) []MarketItem {
	out := make([]MarketItem, 0, len(list))
	for i := range list {
		c := &list[i]
		e := &c.Offer.Entity
		photo := services.CoverURL(e)
		if photo == "" {
			photo = services.ProfilePath(e)
		}
		var d *float64
		if c.DistanceM >= 0 {
			v := c.DistanceM
			d = &v
		}
		out = append(out, MarketItem{
			OfferResponse: dtos.OfferResponseFrom(&c.Offer), EntityName: e.Name, CategoryName: e.Category.Name,
			CategoryColor: e.Category.Color, CategoryIcon: e.Category.Icon, Photo: photo, DistanceMeters: d,
			Reason: c.Reason, Trending: c.Trend >= services.MarketTrendMin,
		})
	}
	return out
}

func marketPlace(c *gin.Context) (lat, long float64, hasLoc bool) {
	lat, errLat := strconv.ParseFloat(c.Query("lat"), 64)
	long, errLong := strconv.ParseFloat(c.Query("long"), 64)
	hasLoc = errLat == nil && errLong == nil && !(lat == 0 && long == 0) && lat >= -90 && lat <= 90 && long >= -180 && long <= 180
	return lat, long, hasLoc
}

func marketZones(list []services.MarketZone) []MarketZoneItem {
	out := make([]MarketZoneItem, 0, len(list))
	for _, z := range list {
		var d *float64
		if z.DistanceM >= 0 {
			v := z.DistanceM
			d = &v
		}
		looks := make([]MarketZoneLook, 0, len(z.Looks))
		for _, l := range z.Looks {
			looks = append(looks, MarketZoneLook{Color: l.Color, Icon: l.Icon})
		}
		out = append(out, MarketZoneItem{ID: z.Zone.ID, Name: z.Zone.Name, DistanceMeters: d, Here: z.Here, Looks: looks})
	}
	return out
}

// Market godoc
// @Summary  Vitrina de ofertas de la ciudad: cerca de ti, lo más buscado, tendencia, recomendado y zonas (3.38, 3.39)
// @Tags     offers
// @Produce  json
// @Param    lat query number false "Latitud (decide la ciudad; sin ella no hay «cerca» ni distancias)"
// @Param    long query number false "Longitud"
// @Param    city query string false "Última ciudad conocida (solo se usa sin ubicación)"
// @Param    category query string false "Categorías, separadas por coma"
// @Param    term query string false "Término elegido de «Lo más buscado»"
// @Success  200 {object} MarketResponse
// @Router   /offers/market [get]
func (h *OfferMarketHandler) Market(c *gin.Context) {
	lat, long, hasLoc := marketPlace(c)
	var viewer *uuid.UUID
	if v, ok := c.Get("userID"); ok {
		if id, ok := v.(uuid.UUID); ok {
			viewer = &id
		}
	}
	res, err := h.service.Market(viewer, lat, long, hasLoc, c.Query("city"), c.Query("category"), c.Query("term"), time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo armar la vitrina"})
		return
	}
	s := res.Sections
	zone := ""
	if res.Here != nil {
		zone = res.Here.Name
	}
	c.JSON(http.StatusOK, MarketResponse{
		HasLocation: hasLoc,
		City:        res.City,
		Zone:        zone,
		Zones:       marketZones(res.Zones),
		Near:        marketItems(s.Near),
		Searched:    marketSearched{Terms: nonNilTerms(res.Terms), Term: res.Term, Items: marketItems(s.Searched)},
		Trending:    marketItems(s.Trending),
		ForYou:      marketForYou{TopRated: s.TopRated, Items: marketItems(s.ForYou)},
	})
}

// MarketAll godoc
// @Summary  Todas las ofertas vigentes de la ciudad, por tandas de 20 (3.39)
// @Tags     offers
// @Produce  json
// @Param    lat query number false "Latitud"
// @Param    long query number false "Longitud"
// @Param    city query string false "Última ciudad conocida (solo se usa sin ubicación)"
// @Param    category query string false "Categorías, separadas por coma"
// @Param    zone query string false "Id de zona"
// @Param    q query string false "Búsqueda en título, negocio, categoría y servicios"
// @Param    sort query string false "near | ends | save"
// @Param    cursor query string false "next_cursor de la tanda anterior"
// @Success  200 {object} MarketAllResponse
// @Failure  400 {object} map[string]string
// @Router   /offers/market/all [get]
func (h *OfferMarketHandler) MarketAll(c *gin.Context) {
	lat, long, hasLoc := marketPlace(c)
	var zone *uuid.UUID
	if raw := c.Query("zone"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "zona inválida"})
			return
		}
		zone = &id
	}
	offset := 0
	if raw := c.Query("cursor"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cursor inválido"})
			return
		}
		offset = n
	}
	sortKey := c.DefaultQuery("sort", "near")
	if sortKey != "ends" && sortKey != "save" {
		sortKey = "near"
	}
	q := c.Query("q")
	if len([]rune(q)) > 80 {
		q = string([]rune(q)[:80])
	}
	res, err := h.service.MarketAll(lat, long, hasLoc, c.Query("city"), c.Query("category"), zone, q, sortKey, offset, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudieron cargar las ofertas"})
		return
	}
	next := ""
	if res.Next >= 0 {
		next = strconv.Itoa(res.Next)
	}
	c.JSON(http.StatusOK, MarketAllResponse{City: res.City, Items: marketItems(res.Items), NextCursor: next})
}

func nonNilTerms(t []services.MarketTerm) []services.MarketTerm {
	if t == nil {
		return []services.MarketTerm{}
	}
	return t
}
