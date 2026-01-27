package handlers

import (
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EntityHandler struct {
	Service        *services.EntityService
	StorageService *services.StorageService
	DB             *gorm.DB
}

func NewEntityHandler(service *services.EntityService, storageService *services.StorageService, db *gorm.DB) *EntityHandler {
	return &EntityHandler{
		Service:        service,
		StorageService: storageService,
		DB:             db,
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

func (h *EntityHandler) FindAll(c *gin.Context) {
	latStr := c.Query("lat")
	longStr := c.Query("long")
	radiusStr := c.Query("radius")
	categoryID := c.Query("category")

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

	entities, err := h.Service.FindAll(lat, long, radius, categoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, entities)
}

func (h *EntityHandler) FindAllByOwner(c *gin.Context) {
	userID, _ := c.Get("userID")
	entities, err := h.Service.Repo.FindAllByOwner(userID.(uuid.UUID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entities)
}

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

	// 3. Process Upload
	ext := strings.ToLower(filepath.Ext(file.Filename))
	s3Key := fmt.Sprintf("entities/%s/%s/%s%s", entityID.String(), imageType, uuid.New().String(), ext)

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not open file"})
		return
	}
	defer f.Close()

	contentType := file.Header.Get("Content-Type")
	err = h.StorageService.UploadFile(s3Key, f, contentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload to S3", "details": err.Error()})
		return
	}

	// 4. Create Media mapping
	media := models.Media{
		S3Key:        s3Key,
		OriginalName: file.Filename,
		ContentType:  contentType,
		Size:         file.Size,
	}

	if err := h.DB.Create(&media).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image metadata"})
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
		"url":  proxyURL,
		"type": imageType,
	})
}
