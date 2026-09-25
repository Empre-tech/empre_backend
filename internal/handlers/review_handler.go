package handlers

import (
	"net/http"
	"strconv"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ReviewHandler struct {
	Service     *services.ReviewService
	UserService *services.UserService
}

func NewReviewHandler(service *services.ReviewService, userService *services.UserService) *ReviewHandler {
	return &ReviewHandler{Service: service, UserService: userService}
}

type UpsertReviewRequest struct {
	Rating  int    `json:"rating" binding:"required,min=1,max=5"`
	Comment string `json:"comment"`
}

// Upsert creates or updates the caller's review for a business.
// @Summary Create or update my review
// @Description One review per user per business: writing again replaces the previous rating/comment.
// @Tags Reviews
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Param request body UpsertReviewRequest true "Rating and comment"
// @Success 200 {object} dtos.ReviewResponse
// @Failure 400 {object} map[string]string
// @Router /api/entities/{id}/reviews [post]
func (h *ReviewHandler) Upsert(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	var req UpsertReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	review, err := h.Service.Upsert(entityID, userID, req.Rating, req.Comment)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response := dtos.ReviewResponse{
		ID:        review.ID,
		Rating:    review.Rating,
		Comment:   review.Comment,
		CreatedAt: review.CreatedAt,
		UpdatedAt: review.UpdatedAt,
	}
	if user, err := h.UserService.FindByID(userID); err == nil && user != nil {
		response.User = dtos.ReviewUser{ID: user.ID, Name: user.Name, ProfilePictureURL: user.ProfilePictureURL}
	}

	c.JSON(http.StatusOK, response)
}

// FindByEntity lists reviews for a business, newest first, with the aggregate rating.
// @Summary List reviews for a business
// @Tags Reviews
// @Produce json
// @Param id path string true "Entity ID"
// @Param page query int false "Page number" default(1)
// @Param pageSize query int false "Items per page" default(20)
// @Success 200 {object} map[string]interface{}
// @Router /api/entities/{id}/reviews [get]
func (h *ReviewHandler) FindByEntity(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	reviews, total, err := h.Service.FindByEntity(entityID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := make([]dtos.ReviewResponse, 0, len(reviews))
	for _, review := range reviews {
		user := review.User
		h.UserService.PopulateProfileURL(&user)
		response = append(response, dtos.ReviewResponse{
			ID:        review.ID,
			Rating:    review.Rating,
			Comment:   review.Comment,
			CreatedAt: review.CreatedAt,
			UpdatedAt: review.UpdatedAt,
			User: dtos.ReviewUser{
				ID:                user.ID,
				Name:              user.Name,
				ProfilePictureURL: user.ProfilePictureURL,
			},
		})
	}

	avg, count, _ := h.Service.Summary(entityID)

	c.JSON(http.StatusOK, gin.H{
		"data":    response,
		"summary": dtos.ReviewSummary{Average: avg, Count: count},
		"meta":    gin.H{"total": total, "page": page, "page_size": pageSize},
	})
}

// FindMine returns the caller's own review for a business, if any.
// @Summary Get my review
// @Tags Reviews
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} dtos.ReviewResponse
// @Failure 404 {object} map[string]string
// @Router /api/entities/{id}/reviews/mine [get]
func (h *ReviewHandler) FindMine(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	review, err := h.Service.FindMine(entityID, userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No has dejado una reseña todavía"})
		return
	}

	response := dtos.ReviewResponse{
		ID:        review.ID,
		Rating:    review.Rating,
		Comment:   review.Comment,
		CreatedAt: review.CreatedAt,
		UpdatedAt: review.UpdatedAt,
	}
	if user, err := h.UserService.FindByID(userID); err == nil && user != nil {
		response.User = dtos.ReviewUser{ID: user.ID, Name: user.Name, ProfilePictureURL: user.ProfilePictureURL}
	}

	c.JSON(http.StatusOK, response)
}

// Delete removes the caller's own review for a business.
// @Summary Delete my review
// @Tags Reviews
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} map[string]string
// @Router /api/entities/{id}/reviews [delete]
func (h *ReviewHandler) Delete(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid entity ID"})
		return
	}
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	if err := h.Service.Delete(entityID, userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Reseña eliminada"})
}
