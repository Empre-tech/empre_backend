package handlers

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"empre_backend/internal/services"
)

// ProPlanHandler: Empre Pro del dueño comprado en las tiendas (RevenueCat) y qué negocios cubre (2026-11-26).
type ProPlanHandler struct {
	Service *services.ProPlanService
}

func NewProPlanHandler(s *services.ProPlanService) *ProPlanHandler {
	return &ProPlanHandler{Service: s}
}

type revenueCatWebhook struct {
	Event services.RevenueCatEvent `json:"event"`
}

// RevenueCatWebhook: POST /api/payments/revenuecat/webhook. Público (lo llama RevenueCat); se protege con el secreto
// que RevenueCat manda en la cabecera Authorization. Responde 200 aunque no haya nada que aplicar, para que no reintente.
func (h *ProPlanHandler) RevenueCatWebhook(c *gin.Context) {
	if !h.Service.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no configurado"})
		return
	}
	if err := h.Service.CheckWebhookAuth(c.GetHeader("Authorization")); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var body revenueCatWebhook
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid"})
		return
	}
	applied, err := h.Service.HandleRevenueCat(body.Event, time.Now())
	if err != nil {
		// 500: RevenueCat reintenta más tarde.
		log.Printf("revenuecat: %s %s: %v", body.Event.Type, body.Event.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "retry"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "applied": applied})
}

// Overview: GET /api/users/me/pro. El plan del dueño, sus negocios y cuáles cubre.
func (h *ProPlanHandler) Overview(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	out, err := h.Service.Overview(userID, time.Now())
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

type proCoverageRequest struct {
	EntityIDs []uuid.UUID `json:"entity_ids"`
}

// SetCoverage: PUT /api/users/me/pro/coverage {"entity_ids": [...]}. Qué negocios cubre el plan.
func (h *ProPlanHandler) SetCoverage(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)
	var req proCoverageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Elige los negocios."})
		return
	}
	out, err := h.Service.SetCoverage(userID, req.EntityIDs, time.Now())
	switch {
	case errors.Is(err, services.ErrProCoverageTooMany):
		c.JSON(http.StatusBadRequest, gin.H{"error": "Tu plan no cubre tantos negocios. Sube de plan para sumar otro."})
	case errors.Is(err, services.ErrProCoverageNotOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": "Solo puedes elegir tus propios negocios."})
	case err != nil:
		internalError(c, err)
	default:
		c.JSON(http.StatusOK, out)
	}
}
