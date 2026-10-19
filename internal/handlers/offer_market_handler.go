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

type MarketResponse struct {
	HasLocation bool           `json:"has_location"`
	Near        []MarketItem   `json:"near"`
	Searched    marketSearched `json:"searched"`
	Trending    []MarketItem   `json:"trending"`
	ForYou      marketForYou   `json:"for_you"`
	All         []MarketItem   `json:"all"`
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

// Market godoc
// @Summary  Vitrina de ofertas: cerca de ti, lo más buscado, tendencia, recomendado y todas (3.38)
// @Tags     offers
// @Produce  json
// @Param    lat query number false "Latitud (sin ella no hay «cerca» ni distancias)"
// @Param    long query number false "Longitud"
// @Param    category query string false "Categorías, separadas por coma"
// @Param    term query string false "Término elegido de «Lo más buscado»"
// @Param    sort query string false "near | ends | save"
// @Success  200 {object} MarketResponse
// @Router   /offers/market [get]
func (h *OfferMarketHandler) Market(c *gin.Context) {
	lat, errLat := strconv.ParseFloat(c.Query("lat"), 64)
	long, errLong := strconv.ParseFloat(c.Query("long"), 64)
	hasLoc := errLat == nil && errLong == nil && !(lat == 0 && long == 0) && lat >= -90 && lat <= 90 && long >= -180 && long <= 180
	var viewer *uuid.UUID
	if v, ok := c.Get("userID"); ok {
		if id, ok := v.(uuid.UUID); ok {
			viewer = &id
		}
	}
	sortKey := c.DefaultQuery("sort", "near")
	if sortKey != "ends" && sortKey != "save" {
		sortKey = "near"
	}
	res, err := h.service.Market(viewer, lat, long, hasLoc, c.Query("category"), c.Query("term"), sortKey, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo armar la vitrina"})
		return
	}
	s := res.Sections
	c.JSON(http.StatusOK, MarketResponse{
		HasLocation: hasLoc,
		Near:        marketItems(s.Near),
		Searched:    marketSearched{Terms: nonNilTerms(res.Terms), Term: res.Term, Items: marketItems(s.Searched)},
		Trending:    marketItems(s.Trending),
		ForYou:      marketForYou{TopRated: s.TopRated, Items: marketItems(s.ForYou)},
		All:         marketItems(s.All),
	})
}

func nonNilTerms(t []services.MarketTerm) []services.MarketTerm {
	if t == nil {
		return []services.MarketTerm{}
	}
	return t
}
