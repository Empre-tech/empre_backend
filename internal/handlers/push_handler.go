package handlers

import (
	"net/http"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PushHandler struct {
	Service *services.PushService
}

func NewPushHandler(service *services.PushService) *PushHandler {
	return &PushHandler{Service: service}
}

type PushTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

// Register saves (or reassigns) the caller's Expo push token for this device.
// @Summary Register a push notification token
// @Tags Push
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body PushTokenRequest true "Expo push token"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/users/push-token [post]
func (h *PushHandler) Register(c *gin.Context) {
	var req PushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	if err := h.Service.SaveToken(userID, req.Token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Token registrado"})
}

// Remove unregisters a push token (e.g. on logout), so this device stops
// receiving notifications for an account no longer signed in on it.
// @Summary Remove a push notification token
// @Tags Push
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body PushTokenRequest true "Expo push token"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/users/push-token [delete]
func (h *PushHandler) Remove(c *gin.Context) {
	var req PushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.Service.RemoveToken(req.Token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Token eliminado"})
}
