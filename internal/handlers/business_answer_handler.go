package handlers

import (
	"errors"
	"net/http"
	"time"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BusinessAnswerHandler: lo que el dueño le enseña a Empre AI, lo que Empre AI no supo responder, el interruptor y
// «suele responder en» (3.62, F3).
type BusinessAnswerHandler struct {
	service *services.BusinessAnswerService
}

func NewBusinessAnswerHandler(s *services.BusinessAnswerService) *BusinessAnswerHandler {
	return &BusinessAnswerHandler{service: s}
}

func answerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrNotOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio no encontrado"})
	case errors.Is(err, services.ErrAnswerInput), errors.Is(err, services.ErrAIIntent), errors.Is(err, services.ErrTooManyAnswer):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Algo falló. Inténtalo de nuevo."})
	}
}

func entityAndUser(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id inválido"})
		return uuid.Nil, uuid.Nil, false
	}
	v, ok := c.Get("userID")
	if !ok {
		return entityID, uuid.Nil, true
	}
	return entityID, v.(uuid.UUID), true
}

// List: público. GET /api/entities/:id/answers
func (h *BusinessAnswerHandler) List(c *gin.Context) {
	entityID, _, ok := entityAndUser(c)
	if !ok {
		return
	}
	list, off, err := h.service.List(entityID)
	if err != nil {
		answerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list, "ai_chat_off": off})
}

// Upsert: el dueño. PUT /api/entities/:id/answers
func (h *BusinessAnswerHandler) Upsert(c *gin.Context) {
	entityID, userID, ok := entityAndUser(c)
	if !ok {
		return
	}
	var in services.BusinessAnswerInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := h.service.Upsert(entityID, userID, in)
	if err != nil {
		answerError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// Delete: el dueño. DELETE /api/entities/:id/answers/:answerId
func (h *BusinessAnswerHandler) Delete(c *gin.Context) {
	entityID, userID, ok := entityAndUser(c)
	if !ok {
		return
	}
	answerID, err := uuid.Parse(c.Param("answerId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "answerId inválido"})
		return
	}
	if err := h.service.Delete(entityID, userID, answerID); err != nil {
		answerError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Unanswered: el dueño. GET /api/entities/:id/answers/unanswered
func (h *BusinessAnswerHandler) Unanswered(c *gin.Context) {
	entityID, userID, ok := entityAndUser(c)
	if !ok {
		return
	}
	list, err := h.service.Unanswered(entityID, userID, time.Now())
	if err != nil {
		answerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": list})
}

// SetChat: el dueño prende o apaga las respuestas de Empre AI. PATCH /api/entities/:id/answers/chat {"off": bool}
func (h *BusinessAnswerHandler) SetChat(c *gin.Context) {
	entityID, userID, ok := entityAndUser(c)
	if !ok {
		return
	}
	var in struct {
		Off bool `json:"off"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.service.SetChatOff(entityID, userID, in.Off); err != nil {
		answerError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ai_chat_off": in.Off})
}

// ReplyTime: público. GET /api/entities/:id/reply-time → {"minutes": n} o {"minutes": null} si no hay datos.
func (h *BusinessAnswerHandler) ReplyTime(c *gin.Context) {
	entityID, _, ok := entityAndUser(c)
	if !ok {
		return
	}
	m, has, err := h.service.ReplyMinutes(entityID, time.Now())
	if err != nil {
		answerError(c, err)
		return
	}
	if !has {
		c.JSON(http.StatusOK, gin.H{"minutes": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"minutes": m})
}
