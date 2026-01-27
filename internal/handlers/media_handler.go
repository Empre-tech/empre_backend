package handlers

import (
	"empre_backend/internal/services"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type MediaHandler struct {
	Service *services.MediaService
}

func NewMediaHandler(service *services.MediaService) *MediaHandler {
	return &MediaHandler{
		Service: service,
	}
}

// FindMedia proxies the image from S3 to the client using a secure mapping
// FindMedia retrieves an image from S3 via a secure mapping
// @Summary Get image by secure ID
// @Description Fetch image binary data using its secure UUID mapping
// @Tags Media
// @Produce image/png,image/jpeg,image/webp
// @Param id path string true "Media ID"
// @Success 200 {file} binary
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/images/{id} [get]
func (h *MediaHandler) FindMedia(c *gin.Context) {
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Image ID format"})
		return
	}

	// 1. Fetch from S3 via MediaService (which handles the ID -> S3Key lookup)
	body, contentType, err := h.Service.GetFile(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Physical image file not found or metadata missing"})
		return
	}
	defer body.Close()

	if contentType != "" {
		c.Header("Content-Type", contentType)
	}

	c.Header("Cache-Control", "public, max-age=31536000")
	io.Copy(c.Writer, body)
}

// Upload handles manual uploads and creates a secure mapping entry
// Upload handles manual image uploads
// @Summary Upload a general image
// @Description Upload an image to S3 and get a secure UUID mapping
// @Tags Media
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "Image File"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/images/upload [post]
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

	// 1. Process Upload and Map via MediaService
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not open file"})
		return
	}
	defer f.Close()

	media, err := h.Service.UploadAndMap("uploads", file.Filename, f, file.Header.Get("Content-Type"), file.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload to S3", "details": err.Error()})
		return
	}

	// 2. Return the SECURE Proxy URL
	proxyURL := fmt.Sprintf("/api/images/%s", media.ID.String())

	c.JSON(http.StatusOK, gin.H{
		"id":  media.ID,
		"url": proxyURL,
	})
}
