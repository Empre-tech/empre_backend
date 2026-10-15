package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"empre_backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type FavoriteHandler struct {
	Service       *services.FavoriteService
	EntityService *services.EntityService
	ReviewService *services.ReviewService
	PushService   *services.PushService
	SignalService *services.SignalService
}

func NewFavoriteHandler(service *services.FavoriteService, entityService *services.EntityService, reviewService *services.ReviewService, pushService *services.PushService, signalService *services.SignalService) *FavoriteHandler {
	return &FavoriteHandler{Service: service, EntityService: entityService, ReviewService: reviewService, PushService: pushService, SignalService: signalService}
}

// Add marks a business as one of the caller's favorites.
// @Summary Add a business to favorites
// @Tags Favorites
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} map[string]string
// @Router /api/entities/{id}/favorite [post]
func (h *FavoriteHandler) Add(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	if err := h.Service.Add(userID, entityID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if h.SignalService != nil {
		h.SignalService.Record(entityID, models.SignalSave)
	}

	if h.PushService != nil {
		if entity, err := h.EntityService.FindByID(entityID); err == nil && entity.OwnerID != userID {
			h.PushService.Notify(entity.OwnerID, "Nuevo favorito",
				fmt.Sprintf("Alguien agregó %s a sus favoritos.", entity.Name),
				map[string]string{"type": "favorite", "entity_id": entity.ID.String()})
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Agregado a favoritos"})
}

// Remove unmarks a business as one of the caller's favorites.
// @Summary Remove a business from favorites
// @Tags Favorites
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} map[string]string
// @Router /api/entities/{id}/favorite [delete]
func (h *FavoriteHandler) Remove(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	if err := h.Service.Remove(userID, entityID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Quitado de favoritos"})
}

// FindMine lists the businesses the caller has favorited.
// @Summary List my favorite businesses
// @Tags Favorites
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param pageSize query int false "Items per page" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /api/users/me/favorites [get]
func (h *FavoriteHandler) FindMine(c *gin.Context) {
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	entities, total, err := h.Service.FindEntitiesByUser(userID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.EntityService.PopulateMediaURLs(entities)

	entityIDs := make([]uuid.UUID, len(entities))
	for i, e := range entities {
		entityIDs[i] = e.ID
	}
	ratingSummaries, _ := h.ReviewService.Summaries(entityIDs)

	response := make([]dtos.EntityMapDTO, 0, len(entities))
	for _, e := range entities {
		summary := ratingSummaries[e.ID]
		response = append(response, dtos.EntityMapDTO{
			ID:           e.ID,
			Name:         e.Name,
			CategoryID:   e.Category.ID,
			CategoryName: e.Category.Name,
			CategoryIcon: e.Category.Icon,
			ProfileURL:   e.ProfileURL,
			Latitude:     e.Latitude,
			Longitude:    e.Longitude,
			IsVerified:   e.IsVerified,
			AvgRating:    summary.Avg,
			ReviewCount:  summary.Count,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": response,
		"meta": gin.H{"total": total, "page": page, "page_size": pageSize},
	})
}

// Novedades lists what the businesses the caller follows published after the
// caller started following them (last 30 days), newest first.
// @Router /api/users/me/novedades [get]
func (h *FavoriteHandler) Novedades(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)

	items, err := h.Service.Novedades(userID, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Datos de los negocios seguidos para mostrar nombre y foto en cada novedad.
	entities, _, err := h.Service.FindEntitiesByUser(userID, 1, 1000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.EntityService.PopulateMediaURLs(entities)
	byID := make(map[uuid.UUID]models.Entity, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
	}

	type novedadDTO struct {
		Kind             string    `json:"kind"`
		ID               string    `json:"id"`
		EntityID         uuid.UUID `json:"entity_id"`
		EntityName       string    `json:"entity_name"`
		EntityProfileURL string    `json:"entity_profile_url"`
		At               time.Time `json:"at"`
		Title            string    `json:"title"`
		Unseen           bool      `json:"unseen"`
	}
	out := make([]novedadDTO, 0, len(items))
	unseen := 0
	for _, n := range items {
		e, ok := byID[n.EntityID]
		if !ok {
			continue
		}
		if n.Unseen {
			unseen++
		}
		out = append(out, novedadDTO{Kind: n.Kind, ID: n.ID, EntityID: n.EntityID, EntityName: e.Name, EntityProfileURL: e.ProfileURL, At: n.At, Title: n.Title, Unseen: n.Unseen})
	}

	c.JSON(http.StatusOK, gin.H{"data": out, "unseen_total": unseen})
}

// MarkNovedadesSeen marks the caller's novedades as seen, for one followed
// business (entity_id) or for all of them when it is omitted.
// @Router /api/users/me/novedades/seen [post]
func (h *FavoriteHandler) MarkNovedadesSeen(c *gin.Context) {
	userID := c.MustGet("userID").(uuid.UUID)

	var body struct {
		EntityID string `json:"entity_id"`
	}
	_ = c.ShouldBindJSON(&body)

	var entityID *uuid.UUID
	if body.EntityID != "" {
		id, err := uuid.Parse(body.EntityID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
			return
		}
		entityID = &id
	}
	if err := h.Service.MarkNovedadesSeen(userID, entityID, utils.NowInCartagena()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Novedades vistas"})
}

// FollowerCount tells the owner how many people follow the business (never who).
// @Router /api/entities/{id}/followers [get]
func (h *FavoriteHandler) FollowerCount(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	userID := c.MustGet("userID").(uuid.UUID)

	entity, err := h.EntityService.FindByID(entityID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio no encontrado"})
		return
	}
	if entity.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Solo el dueño puede ver sus seguidores"})
		return
	}
	count, err := h.Service.CountFollowers(entityID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"followers": count})
}
