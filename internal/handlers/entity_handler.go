package handlers

import (
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaginationMeta contains metadata for paginated responses
type PaginationMeta struct {
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// EntityPaginatedResponse is the top-level response for paginated entity queries
type EntityPaginatedResponse struct {
	Data []models.Entity `json:"data"`
	Meta PaginationMeta  `json:"meta"`
}

type EntityHandler struct {
	Service      *services.EntityService
	MediaService *services.MediaService
	DB           *gorm.DB
}

func NewEntityHandler(service *services.EntityService, mediaService *services.MediaService, db *gorm.DB) *EntityHandler {
	return &EntityHandler{
		Service:      service,
		MediaService: mediaService,
		DB:           db,
	}
}

type CreateEntityRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Address     string   `json:"address"`
	City        string   `json:"city"`
	Latitude    float64  `json:"latitude"`
	Longitude   float64  `json:"longitude"`
	BannerURL   string   `json:"banner_url"`
	ProfileURL  string   `json:"profile_url"`
	Gallery     []string `json:"gallery"` // List of Media IDs (UUIDs)
}

// Create handles entity creation
// @Summary Create a new entity
// @Description Register a new business entity with basic details
// @Tags Entities
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateEntityRequest true "Entity Info"
// @Success 201 {object} models.Entity
// @Failure 401 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Router /api/entities [post]
func (h *EntityHandler) Create(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req CreateEntityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Parse CategoryID
	categoryID, err := uuid.Parse(req.Category)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Category ID format"})
		return
	}

	entity := models.Entity{
		OwnerID:     userID.(uuid.UUID),
		Name:        req.Name,
		Description: req.Description,
		CategoryID:  categoryID,
		Address:     req.Address,
		City:        req.City,
		Latitude:    req.Latitude,
		Longitude:   req.Longitude,
		BannerURL:   req.BannerURL,
		ProfileURL:  req.ProfileURL,
	}

	// Handle Gallery
	if len(req.Gallery) > 0 {
		for i, idStr := range req.Gallery {
			mediaID, err := uuid.Parse(idStr)
			if err == nil {
				entity.Photos = append(entity.Photos, models.EntityPhoto{
					MediaID: mediaID,
					Order:   i,
				})
			}
		}
	}

	if err := h.Service.CreateEntity(&entity); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, entity)
}

// FindByID retrieves an entity by its UUID
// @Summary Find entity by ID
// @Description Get full details of a specific business entity
// @Tags Entities
// @Produce json
// @Param id path string true "Entity ID"
// @Success 200 {object} models.Entity
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/entities/{id} [get]
func (h *EntityHandler) FindByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}

	entity, err := h.Service.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
		return
	}

	c.JSON(http.StatusOK, entity)
}

// FindAll retrieves all entities with optional filters
// @Summary Find all entities
// @Description Search entities with geographic and category filters
// @Tags Entities
// @Produce json
// @Param lat query number false "Latitude"
// @Param long query number false "Longitude"
// @Param radius query number false "Radius in meters"
// @Param category query string false "Category UUID"
// @Param page query int false "Page number" default(1)
// @Param pageSize query int false "Items per page" default(20)
// @Success 200 {object} EntityPaginatedResponse
// @Failure 500 {object} map[string]string
// @Router /api/entities [get]
func (h *EntityHandler) FindAll(c *gin.Context) {
	latStr := c.Query("lat")
	longStr := c.Query("long")
	radiusStr := c.Query("radius")
	categoryID := c.Query("category")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	var lat, long, radius float64

	if latStr != "" {
		lat, _ = strconv.ParseFloat(latStr, 64)
	}
	if longStr != "" {
		long, _ = strconv.ParseFloat(longStr, 64)
	}
	if radiusStr != "" {
		radius, _ = strconv.ParseFloat(radiusStr, 64)
	}

	entities, total, err := h.Service.FindAll(lat, long, radius, categoryID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, EntityPaginatedResponse{
		Data: entities,
		Meta: PaginationMeta{
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		},
	})
}

// FindAllByOwner retrieves all entities for the authenticated owner
// @Summary Find my entities
// @Description Get all business entities owned by the current user
// @Tags Entities
// @Produce json
// @Security BearerAuth
// @Success 200 {array} models.Entity
// @Failure 500 {object} map[string]string
// @Router /api/entities/mine [get]
func (h *EntityHandler) FindAllByOwner(c *gin.Context) {
	userID, _ := c.Get("userID")
	entities, err := h.Service.FindAllByOwner(userID.(uuid.UUID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entities)
}

// Update modifies an existing entity
// @Summary Update entity
// @Description Update details of a specific business entity (Owner only)
// @Tags Entities
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Param request body CreateEntityRequest true "Update Info"
// @Success 200 {object} models.Entity
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/entities/{id} [put]
func (h *EntityHandler) Update(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	userID, _ := c.Get("userID")

	// Ownership check
	existing, err := h.Service.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
		return
	}
	if existing.OwnerID != userID.(uuid.UUID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized to update this entity"})
		return
	}

	var req CreateEntityRequest // Reuse same struct for simplicity
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.Address = req.Address
	existing.City = req.City
	existing.Latitude = req.Latitude
	existing.Longitude = req.Longitude
	existing.BannerURL = req.BannerURL
	existing.ProfileURL = req.ProfileURL

	if req.Category != "" {
		catID, _ := uuid.Parse(req.Category)
		existing.CategoryID = catID
	}

	// Simple gallery replacement strategy:
	// In a real app, you might want more granular sync (add/remove/reorder),
	// but for now, we'll replace the whole list if provided.
	if len(req.Gallery) > 0 {
		var newPhotos []models.EntityPhoto
		for i, idStr := range req.Gallery {
			mediaID, err := uuid.Parse(idStr)
			if err == nil {
				newPhotos = append(newPhotos, models.EntityPhoto{
					EntityID: existing.ID,
					MediaID:  mediaID,
					Order:    i,
				})
			}
		}
		existing.Photos = newPhotos
	}

	if err := h.Service.UpdateEntity(existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, existing)
}

// Delete removes an existing entity
// @Summary Delete entity
// @Description Remove a specific business entity (Owner only)
// @Tags Entities
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/entities/{id} [delete]
func (h *EntityHandler) Delete(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	userID, _ := c.Get("userID")

	// Ownership check
	existing, err := h.Service.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
		return
	}
	if existing.OwnerID != userID.(uuid.UUID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized to delete this entity"})
		return
	}

	if err := h.Service.DeleteEntity(existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Entity deleted successfully"})
}

// UploadImage handles direct image uploads for an entity
// @Summary Upload entity image
// @Description Upload a profile, banner, or gallery image for a specific entity
// @Tags Entities
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param id path string true "Entity ID"
// @Param file formData file true "Image File"
// @Param type formData string true "Image Type (profile, banner, gallery)"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/entities/{id}/images [post]
func (h *EntityHandler) UploadImage(c *gin.Context) {
	idStr := c.Param("id")
	entityID, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Entity ID"})
		return
	}

	userID, _ := c.Get("userID")

	// 1. Ownership Check
	existing, err := h.Service.FindByID(entityID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
		return
	}
	if existing.OwnerID != userID.(uuid.UUID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized to update this entity"})
		return
	}

	// 2. Get file and type
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	imageType := c.PostForm("type") // profile, banner, gallery
	if imageType != "profile" && imageType != "banner" && imageType != "gallery" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid image type. Use: profile, banner, or gallery"})
		return
	}

	// 3. Process Upload via MediaService
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not open file"})
		return
	}
	defer f.Close()

	folder := fmt.Sprintf("entities/%s/%s", entityID.String(), imageType)
	media, err := h.MediaService.UploadAndMap(folder, file.Filename, f, file.Header.Get("Content-Type"), file.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload to S3", "details": err.Error()})
		return
	}

	proxyURL := fmt.Sprintf("/api/images/%s", media.ID.String())

	// 5. Update Entity
	switch imageType {
	case "profile":
		existing.ProfileURL = proxyURL
		h.Service.UpdateEntity(existing)
	case "banner":
		existing.BannerURL = proxyURL
		h.Service.UpdateEntity(existing)
	case "gallery":
		photo := models.EntityPhoto{
			EntityID: entityID,
			MediaID:  media.ID,
		}
		h.DB.Create(&photo)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":   media.ID,
		"url":  media.URL,
		"type": imageType,
	})
}
