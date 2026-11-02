package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type AskHandler struct {
	Service *services.AskService
}

func NewAskHandler(service *services.AskService) *AskHandler {
	return &AskHandler{Service: service}
}

// Ask: «Pregúntale a Empre». Público.
// GET /api/ask?q=almuerzo barato cerca&lat=..&long=..&open=1&offer=1&delivery=1&cheap=1
func (h *AskHandler) Ask(c *gin.Context) {
	lat, _ := strconv.ParseFloat(c.Query("lat"), 64)
	lng, _ := strconv.ParseFloat(c.Query("long"), 64)
	on := func(k string) bool { return c.Query(k) == "1" || c.Query(k) == "true" }
	res, err := h.Service.Ask(services.AskInput{
		Question: c.Query("q"),
		Lat:      lat,
		Lng:      lng,
		Filters:  services.AskFilters{OpenNow: on("open"), WithOffer: on("offer"), Delivery: on("delivery"), Cheapest: on("cheap")},
	})
	if errors.Is(err, services.ErrCatalogInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Escribe qué necesitas"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos buscar ahora"})
		return
	}
	c.JSON(http.StatusOK, res)
}
