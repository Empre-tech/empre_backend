package handlers

import (
	"net/http"
	"strconv"
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

// ActiveList returns the active offers of a business (public), soonest-ending first (ties: more free slots).
// `limit` (optional, up to 50) trims the list; `total` says how many there are in all.
// @Router /api/entities/{id}/offers/active [get]
func (h *OfferHandler) ActiveList(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offers, total, err := h.Service.ActivePageForEntity(entityID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]dtos.OfferResponse, 0, len(offers))
	for i := range offers {
		out = append(out, dtos.OfferResponseFrom(&offers[i]))
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "total": total})
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

// Stats devuelve cómo le fue a cada oferta de los últimos 90 días y lo que dicen las ofertas pasadas (solo el dueño).
// @Router /api/entities/{id}/offers/stats [get]
func (h *OfferHandler) Stats(c *gin.Context) {
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
	stats, sum, err := h.Service.Stats(entityID, userID, time.Now())
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	out := make([]dtos.OfferStatResponse, 0, len(stats))
	for _, s := range stats {
		out = append(out, dtos.OfferStatResponse{
			OfferID: s.OfferID, Title: s.Title, Type: string(s.Type), StartsAt: s.StartsAt, EndsAt: s.EndsAt,
			TotalSlots: s.TotalSlots, SpecialPrice: s.SpecialPrice, Ended: s.Ended, Views: s.Views, Claims: s.Claims,
			Redeemed: s.Redeemed, FilledMinutes: s.FilledMinutes, Hour: s.Hour, Weekday: s.Weekday, DurationMinutes: s.DurationMinutes,
		})
	}
	insights := make([]dtos.OfferInsightResponse, 0, len(sum.Insights))
	for _, i := range sum.Insights {
		insights = append(insights, dtos.OfferInsightResponse{Kind: i.Kind, Text: i.Text})
	}
	c.JSON(http.StatusOK, gin.H{"offers": out, "summary": dtos.OfferStatsSummaryResponse{
		Finished: sum.Finished, Views: sum.Views, Claims: sum.Claims, Redeemed: sum.Redeemed, Enough: sum.Enough,
		MinOffers: services.InsightMinOffers, MinViews: services.InsightMinViews, Insights: insights,
		BestOfferID: sum.BestOfferID, PreferBand: sum.PreferBand, PreferShort: sum.PreferShort, AvgFillMinutes: sum.AvgFillMinutes,
	}})
}
