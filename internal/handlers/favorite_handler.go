package handlers

import (
	"net/http"
	"strconv"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type FavoriteHandler struct {
	Service       *services.FavoriteService
	EntityService *services.EntityService
	ReviewService *services.ReviewService
}

func NewFavoriteHandler(service *services.FavoriteService, entityService *services.EntityService, reviewService *services.ReviewService) *FavoriteHandler {
	return &FavoriteHandler{Service: service, EntityService: entityService, ReviewService: reviewService}
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
