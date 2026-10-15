package handlers

import (
	"net/http"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PromotionHandler struct {
	Service *services.PromotionService
}

func NewPromotionHandler(service *services.PromotionService) *PromotionHandler {
	return &PromotionHandler{Service: service}
}

type CreatePromotionRequest struct {
	Title       string    `json:"title" binding:"required"`
	Description string    `json:"description"`
	Price       string    `json:"price"`
	Discount    string    `json:"discount"`
	NormalPrice string    `json:"normal_price"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at" binding:"required"`
	Conditions  string    `json:"conditions"`
	Tags        string    `json:"tags"`
}

type UpdatePromotionRequest struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Price       string    `json:"price"`
	Discount    string    `json:"discount"`
	NormalPrice string    `json:"normal_price"`
	EndsAt      time.Time `json:"ends_at"`
	Conditions  string    `json:"conditions"`
	Tags        string    `json:"tags"`
	Status      string    `json:"status"` // "", "active", "paused", "finished"
}

// Publish crea una promoción (dueño).
// @Router /api/entities/{id}/promotions [post]
func (h *PromotionHandler) Publish(c *gin.Context) {
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

	var req CreatePromotionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p, err := h.Service.Publish(entityID, userID, services.PublishPromotionInput{
		Title:       req.Title,
		Description: req.Description,
		Price:       req.Price,
		Discount:    req.Discount,
		NormalPrice: req.NormalPrice,
		StartsAt:    req.StartsAt,
		EndsAt:      req.EndsAt,
		Conditions:  req.Conditions,
		Tags:        req.Tags,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dtos.PromotionResponseFrom(p))
}

// List devuelve las promociones del negocio (dueño).
// @Router /api/entities/{id}/promotions [get]
func (h *PromotionHandler) List(c *gin.Context) {
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

	ps, err := h.Service.ListByEntity(entityID, userID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	resp := make([]dtos.PromotionResponse, 0, len(ps))
	for i := range ps {
		resp = append(resp, dtos.PromotionResponseFrom(&ps[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// Active devuelve las promociones activas de un negocio (público).
// @Router /api/entities/{id}/promotions/active [get]
func (h *PromotionHandler) Active(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}

	ps, err := h.Service.ActiveForEntity(entityID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := make([]dtos.PromotionResponse, 0, len(ps))
	for i := range ps {
		resp = append(resp, dtos.PromotionResponseFrom(&ps[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// Update edita o cambia el estado de una promoción (dueño).
// @Router /api/promotions/{id} [patch]
func (h *PromotionHandler) Update(c *gin.Context) {
	promotionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	var req UpdatePromotionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	p, err := h.Service.Update(promotionID, userID, services.PublishPromotionInput{
		Title:       req.Title,
		Description: req.Description,
		Price:       req.Price,
		Discount:    req.Discount,
		NormalPrice: req.NormalPrice,
		EndsAt:      req.EndsAt,
		Conditions:  req.Conditions,
		Tags:        req.Tags,
	}, req.Status)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dtos.PromotionResponseFrom(p))
}
