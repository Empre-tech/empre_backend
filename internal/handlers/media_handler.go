package handlers

import (
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MediaHandler struct {
	storageService *services.StorageService
	db             *gorm.DB
}

func NewMediaHandler(storageService *services.StorageService, db *gorm.DB) *MediaHandler {
	return &MediaHandler{
		storageService: storageService,
		db:             db,
	}
}

// FindMedia proxies the image from S3 to the client using a secure mapping
func (h *MediaHandler) FindMedia(c *gin.Context) {
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Image ID format"})
		return
	}

	// 1. Lookup mapping in Database
	var media models.Media
	if err := h.db.First(&media, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Image metadata not found"})
		return
	}

	// 2. Fetch from S3 using the hidden S3Key
	body, contentType, err := h.storageService.GetFile(media.S3Key)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Physical image file not found"})
		return
	}
	defer body.Close()

	if contentType != "" {
		c.Header("Content-Type", contentType)
	} else if media.ContentType != "" {
		c.Header("Content-Type", media.ContentType)
	}

	c.Header("Cache-Control", "public, max-age=31536000")
	io.Copy(c.Writer, body)
}

// Upload handles manual uploads and creates a secure mapping entry
func (h *MediaHandler) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported file format"})
		return
	}

	// 1. Construct internal S3 Key (completely hidden from response)
	s3Key := fmt.Sprintf("uploads/%s%s", uuid.New().String(), ext)

	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not open file"})
		return
	}
	defer f.Close()

	contentType := file.Header.Get("Content-Type")

	// 2. Upload to S3
	err = h.storageService.UploadFile(s3Key, f, contentType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to upload to S3",
			"details": err.Error(),
		})
		return
	}

	// 3. Create Media mapping record
	media := models.Media{
		S3Key:        s3Key,
		OriginalName: file.Filename,
		ContentType:  contentType,
		Size:         file.Size,
	}

	if err := h.db.Create(&media).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save image metadata"})
		return
	}

	// 4. Return the SECURE Proxy URL using ONLY the mapping ID
	proxyURL := fmt.Sprintf("/api/images/%s", media.ID.String())

	c.JSON(http.StatusOK, gin.H{
		"id":  media.ID,
		"url": proxyURL,
	})
}
