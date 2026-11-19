package handlers

import (
	"errors"
	"net/http"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// OfferAlertHandler es el «Avísame» de Ofertas: pedir, ver y quitar avisos.
type OfferAlertHandler struct {
	Service *services.OfferAlertService
}

func NewOfferAlertHandler(service *services.OfferAlertService) *OfferAlertHandler {
	return &OfferAlertHandler{Service: service}
}

type createAlertRequest struct {
	ZoneID     string `json:"zone_id" binding:"required"`
	CategoryID string `json:"category_id"`
}

// Create pide que avisen cuando salga una oferta en la zona (y la categoría, si viene).
// @Router /api/offer-alerts [post]
func (h *OfferAlertHandler) Create(c *gin.Context) {
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}
	var req createAlertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Falta la zona"})
		return
	}
	zoneID, err := uuid.Parse(req.ZoneID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Zona inválida"})
		return
	}
	var cat *uuid.UUID
	if req.CategoryID != "" {
		id, err := uuid.Parse(req.CategoryID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Categoría inválida"})
			return
		}
		cat = &id
	}
	a, err := h.Service.Create(userID, zoneID, cat)
	if errors.Is(err, services.ErrZoneNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudo guardar el aviso"})
		return
	}
	c.JSON(http.StatusCreated, a)
}

// Mine devuelve los avisos de la persona.
// @Router /api/offer-alerts [get]
func (h *OfferAlertHandler) Mine(c *gin.Context) {
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}
	list, err := h.Service.Mine(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudieron leer tus avisos"})
		return
	}
	if list == nil {
		list = []services.AlertView{}
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// Remove quita un aviso.
// @Router /api/offer-alerts/{id} [delete]
func (h *OfferAlertHandler) Remove(c *gin.Context) {
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	if err := h.Service.Remove(userID, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudo quitar el aviso"})
		return
	}
	c.Status(http.StatusNoContent)
}
