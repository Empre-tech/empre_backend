package handlers

import (
	"net/http"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type OfferHandler struct {
	Service *services.OfferService
}

func NewOfferHandler(service *services.OfferService) *OfferHandler {
	return &OfferHandler{Service: service}
}

type CreateOfferRequest struct {
	Type         string    `json:"type" binding:"required"`
	Title        string    `json:"title" binding:"required"`
	Price        string    `json:"price"`
	NormalPrice  string    `json:"normal_price"`
	Conditions   string    `json:"conditions"`
	EndsAt       time.Time `json:"ends_at" binding:"required"`
	TotalSlots   int       `json:"total_slots" binding:"required"`
	RadiusMeters int       `json:"radius_meters"`
	RepeatPolicy string    `json:"repeat_policy"`
	// Servicios del catálogo incluidos en la oferta y precio temporal propio (ambos opcionales).
	ServiceItemIDs []string `json:"service_item_ids"`
	SpecialPrice   *int     `json:"special_price"`
}

type QuickActionRequest struct {
	Action string `json:"action" binding:"required"` // extend | radius | slots
	Value  int    `json:"value" binding:"required"`
}

func ownerID(c *gin.Context) (uuid.UUID, bool) {
	val, exists := c.Get("userID")
	if !exists {
		return uuid.Nil, false
	}
	id, ok := val.(uuid.UUID)
	return id, ok
}

// Publish creates a new offer for a business (owner only).
// @Router /api/entities/{id}/offers [post]
func (h *OfferHandler) Publish(c *gin.Context) {
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

	var req CreateOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	itemIDs := make([]uuid.UUID, 0, len(req.ServiceItemIDs))
	for _, raw := range req.ServiceItemIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "service_item_ids inválido"})
			return
		}
		itemIDs = append(itemIDs, id)
	}

	offer, err := h.Service.Publish(entityID, userID, services.PublishOfferInput{
		ServiceItemIDs: itemIDs,
		SpecialPrice:   req.SpecialPrice,
		Type:           models.OfferType(req.Type),
		Title:          req.Title,
		Price:          req.Price,
		NormalPrice:    req.NormalPrice,
		Conditions:     req.Conditions,
		EndsAt:         req.EndsAt,
		TotalSlots:     req.TotalSlots,
		RadiusMeters:   req.RadiusMeters,
		RepeatPolicy:   models.RepeatPolicy(req.RepeatPolicy),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dtos.OfferResponseFrom(offer))
}

// List returns a business's offers (owner only).
// @Router /api/entities/{id}/offers [get]
func (h *OfferHandler) List(c *gin.Context) {
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

	offers, err := h.Service.ListByEntity(entityID, userID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	resp := make([]dtos.OfferResponse, 0, len(offers))
	for i := range offers {
		resp = append(resp, dtos.OfferResponseFrom(&offers[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": resp})
}

// Active returns the active offer of a business (public).
// @Router /api/entities/{id}/offer [get]
func (h *OfferHandler) Active(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	offer, err := h.Service.ActiveForEntity(entityID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if offer == nil {
		c.JSON(http.StatusOK, gin.H{"data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dtos.OfferResponseFrom(offer)})
}

// ActiveList returns every active offer of a business (public), soonest-ending first.
// @Router /api/entities/{id}/offers/active [get]
func (h *OfferHandler) ActiveList(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	offers, err := h.Service.ActiveListForEntity(entityID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]dtos.OfferResponse, 0, len(offers))
	for i := range offers {
		out = append(out, dtos.OfferResponseFrom(&offers[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

// QuickAction applies extend / radius / slots to an active offer (owner only).
// @Router /api/offers/{id} [patch]
func (h *OfferHandler) QuickAction(c *gin.Context) {
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	var req QuickActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	offer, err := h.Service.QuickAction(offerID, userID, req.Action, req.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dtos.OfferResponseFrom(offer))
}

// Republish clones an offer into a new publication (owner only).
// @Router /api/offers/{id}/republish [post]
func (h *OfferHandler) Republish(c *gin.Context) {
	offerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}

	var req CreateOfferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	offer, err := h.Service.Republish(offerID, userID, services.PublishOfferInput{
		Type:         models.OfferType(req.Type),
		Title:        req.Title,
		Price:        req.Price,
		NormalPrice:  req.NormalPrice,
		Conditions:   req.Conditions,
		EndsAt:       req.EndsAt,
		TotalSlots:   req.TotalSlots,
		RadiusMeters: req.RadiusMeters,
		RepeatPolicy: models.RepeatPolicy(req.RepeatPolicy),
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dtos.OfferResponseFrom(offer))
}
