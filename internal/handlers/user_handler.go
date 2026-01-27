package handlers

import (
	"empre_backend/internal/models"
	"empre_backend/internal/services"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserHandler struct {
	DB             *gorm.DB
	StorageService *services.StorageService
}

func NewUserHandler(db *gorm.DB, storageService *services.StorageService) *UserHandler {
	return &UserHandler{
		DB:             db,
		StorageService: storageService,
	}
}

// GetMe returns the authenticated user's profile
// @Summary Get current user
// @Description Get profile details of the currently authenticated user
// @Tags Users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.User
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/users/me [get]
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, _ := c.Get("userID")

	var user models.User
	if err := h.DB.First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// UploadProfileImage handles user profile picture upload
// @Summary Upload profile image
// @Description Upload a new profile picture for the authenticated user
// @Tags Users
// @Accept multipart/form-data
// @Produce json
// @Security BearerAuth
// @Param file formData file true "Image File"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/users/profile/image [post]
func (h *UserHandler) UploadProfileImage(c *gin.Context) {
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// 1. Process Upload to S3
	ext := strings.ToLower(filepath.Ext(file.Filename))
	s3Key := fmt.Sprintf("users/%s/profile/%s%s", userID.String(), uuid.New().String(), ext)

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

	// 2. Create Media mapping
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

	// 3. Update User
	if err := h.DB.Model(&models.User{}).Where("id = ?", userID).Update("profile_picture_url", proxyURL).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":  media.ID,
		"url": proxyURL,
	})
}
