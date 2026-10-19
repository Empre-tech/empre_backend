package handlers

import (
	"errors"
	"net/http"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CompletenessHandler struct{ service *services.CompletenessService }

func NewCompletenessHandler(s *services.CompletenessService) *CompletenessHandler {
	return &CompletenessHandler{service: s}
}

// Get godoc
// @Summary  «Haz que tu negocio destaque»: los 8 puntos y si están hechos (3.40). Solo el dueño.
// @Tags     entities
// @Produce  json
// @Param    id path string true "Id del negocio"
// @Success  200 {object} map[string][]services.CompletenessItem
// @Failure  403 {object} map[string]string
// @Router   /entities/{id}/completeness [get]
func (h *CompletenessHandler) Get(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}
	items, err := h.service.For(entityID, userID)
	switch {
	case errors.Is(err, services.ErrNotOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio no encontrado"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo calcular"})
	default:
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}
