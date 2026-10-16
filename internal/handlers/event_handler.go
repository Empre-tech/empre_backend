package handlers

import (
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	Service *services.EventService
}

func NewEventHandler(service *services.EventService) *EventHandler {
	return &EventHandler{Service: service}
}

type eventBatchRequest struct {
	// AnonID identifica la instalación (no a la persona).
	AnonID string                `json:"anon_id"`
	Events []services.EventInput `json:"events"`
}

// Ingest recibe un lote de eventos de uso (hasta 20). Público y con límite de peticiones: la app no necesita
// sesión para medir. Responde 202 con cuántos se guardaron; un evento inválido no impide guardar los demás.
// @Router /api/events [post]
func (h *EventHandler) Ingest(c *gin.Context) {
	var req eventBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cuerpo inválido"})
		return
	}
	if len(req.Events) == 0 || len(req.Events) > services.MaxEventsPerBatch {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Envía entre 1 y " + strconv.Itoa(services.MaxEventsPerBatch) + " eventos"})
		return
	}
	res, err := h.Service.Ingest(req.AnonID, req.Events, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudieron guardar los eventos"})
		return
	}
	c.JSON(http.StatusAccepted, res)
}

// Summary cuenta los eventos de los últimos `days` días (por defecto 7, máximo 90). Solo administración.
// @Router /api/admin/events/summary [get]
func (h *EventHandler) Summary(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	rows, err := h.Service.Summary(time.Now().AddDate(0, 0, -days))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudo calcular el resumen"})
		return
	}
	if rows == nil {
		rows = []services.EventCount{}
	}
	c.JSON(http.StatusOK, gin.H{"days": days, "items": rows})
}
