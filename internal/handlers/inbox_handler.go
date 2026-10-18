package handlers

import (
	"net/http"
	"time"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type InboxHandler struct{ service *services.InboxService }

func NewInboxHandler(s *services.InboxService) *InboxHandler { return &InboxHandler{service: s} }

// Summary godoc
// @Summary      Lo pendiente del usuario: conversaciones sin leer y solicitudes de cita por revisar
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} services.InboxSummary
// @Router       /users/me/inbox-summary [get]
func (h *InboxHandler) Summary(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	out, err := h.service.Summary(user, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no se pudo armar el resumen"})
		return
	}
	c.JSON(http.StatusOK, out)
}
