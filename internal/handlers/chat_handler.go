package handlers

import (
	"empre_backend/internal/services"
	"empre_backend/internal/websocket"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ChatHandler struct {
	Hub     *websocket.Hub
	service *services.ChatService
}

func NewChatHandler(hub *websocket.Hub, service *services.ChatService) *ChatHandler {
	return &ChatHandler{
		Hub:     hub,
		service: service,
	}
}

func (h *ChatHandler) HandleWebSocket(c *gin.Context) {
	// Get User ID from context (set by auth middleware)
	userIDVal, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userID := userIDVal.(uuid.UUID)

	websocket.ServeWs(h.Hub, c, userID)
}

func (h *ChatHandler) FindAllConversations(c *gin.Context) {
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	conversations, err := h.service.FindAllConversations(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, conversations)
}

func (h *ChatHandler) FindMessagesHistory(c *gin.Context) {
	entityIDStr := c.Param("entity_id")
	userIDStr := c.Query("user_id")

	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Entity ID format"})
		return
	}

	// Current authenticated user (can be customer or owner)
	currentUserIDVal, _ := c.Get("userID")
	currentUserID := currentUserIDVal.(uuid.UUID)

	var targetUserID uuid.UUID
	if userIDStr != "" {
		// If an owner is requesting history with a specific customer
		targetUserID, err = uuid.Parse(userIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format"})
			return
		}
	} else {
		// If a customer is requesting their own history with an entity
		targetUserID = currentUserID
	}

	messages, err := h.service.FindMessagesHistory(entityID, targetUserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, messages)
}
