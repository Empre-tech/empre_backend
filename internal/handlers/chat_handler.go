package handlers

import (
	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"empre_backend/internal/websocket"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ChatPaginatedResponse struct {
	Data []models.Message `json:"data"`
	Meta PaginationMeta   `json:"meta"`
}

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

// HandleWebSocket initiates a real-time chat connection
// @Summary WebSocket Chat
// @Description Upgrade to WebSocket for real-time messaging
// @Tags Chat
// @Security BearerAuth
// @Param token query string true "JWT Token"
// @Router /api/chat/ws [get]
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

// FindAllConversations retrieves all active conversations for the user with pagination
// @Summary List conversations
// @Description Get a paginated list of the last message from each active conversation, formatted as DTOs
// @Tags Chat
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param pageSize query int false "Items per page" default(20)
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /api/chat/conversations [get]
func (h *ChatHandler) FindAllConversations(c *gin.Context) {
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	conversations, total, err := h.service.FindAllConversations(userID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Cuántos mensajes sin leer hay por conversación (no cuántas conversaciones
	// hay): esto es lo que se muestra como "número de mensajes pendientes" en
	// vez de un simple conteo de conversaciones.
	unreadCounts, err := h.service.UnreadCounts(userID)
	if err != nil {
		log.Println("chat: no pudimos calcular mensajes sin leer:", err)
	}
	unreadByConversation := map[string]int64{}
	for _, row := range unreadCounts {
		unreadByConversation[fmt.Sprintf("%s|%s", row.EntityID, row.UserID)] = row.Unread
	}

	// Transform detailed models into lightweight DTOs
	var response []dtos.ConversationResponse

	for _, msg := range conversations {
		dto := dtos.ConversationResponse{
			ID:           msg.ID,
			EntityID:     msg.EntityID,
			Content:      msg.Content,
			CreatedAt:    msg.CreatedAt,
			IsRead:       msg.IsRead,
			SentByEntity: msg.SentByEntity,
			UnreadCount:  unreadByConversation[fmt.Sprintf("%s|%s", msg.EntityID, msg.UserID)],
		}

		// Determine "Other Party"
		if msg.Entity.OwnerID == userID {
			// I am the owner, talking to a User
			dto.OtherParty = dtos.OtherPartyStats{
				ID:         msg.User.ID,
				Name:       msg.User.Name,
				ProfileURL: msg.User.ProfilePictureURL,
				Type:       "user",
			}
		} else {
			// I am the user, talking to an Entity
			dto.OtherParty = dtos.OtherPartyStats{
				ID:         msg.Entity.ID,
				Name:       msg.Entity.Name,
				ProfileURL: msg.Entity.ProfileURL,
				Type:       "entity",
			}
		}

		response = append(response, dto)
	}

	c.JSON(http.StatusOK, gin.H{
		"data": response,
		"meta": gin.H{
			"total":     total,
			"page":      page,
			"page_size": pageSize,
		},
	})
}

// FindMessagesHistory retrieves full message history between a user and an entity
// @Summary Message history
// @Description Get all messages in a conversation with a specific business
// @Tags Chat
// @Produce json
// @Security BearerAuth
// @Param entity_id path string true "Entity ID"
// @Param user_id query string false "User ID (Owner only usage)"
// @Param page query int false "Page number" default(1)
// @Param pageSize query int false "Items per page" default(50)
// @Success 200 {object} ChatPaginatedResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/chat/history/{entity_id} [get]
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

	// Al abrir esta conversación, se marcan como leídos los mensajes del OTRO
	// lado: si soy el dueño, los del cliente; si soy el cliente, los del
	// negocio. Best-effort: si falla, igual devolvemos el historial (no tiene
	// sentido bloquear la conversación por esto). Si el que los mandó sigue
	// conectado, le avisamos en vivo por WebSocket que ya se los leyeron.
	if recipientID, err := h.service.MarkConversationRead(entityID, targetUserID, currentUserID); err != nil {
		log.Println("chat: no pudimos marcar la conversación como leída:", err)
	} else {
		h.Hub.BroadcastRead(entityID, recipientID)
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

	messages, total, err := h.service.FindMessagesHistory(entityID, targetUserID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ChatPaginatedResponse{
		Data: messages,
		Meta: PaginationMeta{
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		},
	})
}

// GetPresence reports whether the other party of a conversation is
// currently connected (has a live WebSocket), so the chat screen can show a
// real "en línea" / "desconectado" status instead of describing the
// viewer's own connection.
// @Summary Conversation presence
// @Description Whether the other party in this conversation is currently online
// @Tags Chat
// @Produce json
// @Security BearerAuth
// @Param entity_id path string true "Entity ID"
// @Param user_id query string false "User ID (Owner only usage: which customer)"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/chat/presence/{entity_id} [get]
func (h *ChatHandler) GetPresence(c *gin.Context) {
	entityIDStr := c.Param("entity_id")
	entityID, err := uuid.Parse(entityIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Entity ID format"})
		return
	}

	var otherPartyID uuid.UUID
	if userIDStr := c.Query("user_id"); userIDStr != "" {
		// El dueño pregunta por un cliente puntual: la otra parte es ese cliente.
		otherPartyID, err = uuid.Parse(userIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid User ID format"})
			return
		}
	} else if ownerIDStr := c.Query("owner_id"); ownerIDStr != "" {
		// El cliente ya trae el owner_id (lo obtuvo al cargar el negocio): nos lo
		// pasa directo para no pegarle a la base de datos en cada sondeo de presencia.
		otherPartyID, err = uuid.Parse(ownerIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Owner ID format"})
			return
		}
	} else {
		// Respaldo si el cliente no mandó owner_id: el cliente pregunta por el
		// negocio, la otra parte es quien lo administra.
		var entity models.Entity
		if err := h.DB.Select("owner_id").First(&entity, "id = ?", entityID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		otherPartyID = entity.OwnerID
	}

	c.JSON(http.StatusOK, gin.H{"online": h.Hub.IsOnline(otherPartyID)})
}

// SendMessage sends a message via REST and broadcasts it to WebSocket
// @Summary Send a message
// @Tags Chat
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param message body models.Message true "Message content"
// @Success 201 {object} models.Message
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/chat/message [post]
func (h *ChatHandler) SendMessage(c *gin.Context) {
	var msg models.Message
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDVal, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userID := userIDVal.(uuid.UUID)

	// Set sender as current user
	msg.SenderID = userID

	// Save to DB
	if err := h.service.SendMessage(&msg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Broadcast via WebSocket
	jsonData, _ := json.Marshal(msg)
	h.Hub.RouteMessage(&msg, jsonData, userID)

	c.JSON(http.StatusCreated, msg)
}
