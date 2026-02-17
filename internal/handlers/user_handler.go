package handlers

import (
	"empre_backend/internal/dtos"
	"empre_backend/internal/services"
	"empre_backend/pkg/utils"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UserHandler struct {
	Service      *services.UserService
	MediaService *services.MediaService
}

func NewUserHandler(service *services.UserService, mediaService *services.MediaService) *UserHandler {
	return &UserHandler{
		Service:      service,
		MediaService: mediaService,
	}
}

// FindMe returns the authenticated user's profile
// @Summary Get current user
// @Description Get profile details of the currently authenticated user
// @Tags Users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.User
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/users/me [get]
// FindMe returns the authenticated user's profile
// @Summary Get current user
// @Description Get profile details of the currently authenticated user
// @Tags Users
// @Produce json
// @Security BearerAuth
// @Success 200 {object} dtos.UserResponse
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /api/users/me [get]
func (h *UserHandler) FindMe(c *gin.Context) {
	userIDVal, _ := c.Get("userID")
	userID := userIDVal.(uuid.UUID)

	user, err := h.Service.FindByID(userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	response := dtos.UserResponse{
		ID:                user.ID,
		Name:              user.Name,
		Email:             user.Email,
		Phone:             user.Phone,
		ProfilePictureURL: user.ProfilePictureURL,
		Role:              user.Role,
	}

	c.JSON(http.StatusOK, response)
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

	// 1. Validate Image (MIME-type sniffing)
	contentType, err := utils.ValidateImage(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. Process Upload and Map via MediaService
	f, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not open file"})
		return
	}
	defer f.Close()

	folder := fmt.Sprintf("users/%s/profile", userID.String())
	media, err := h.MediaService.UploadAndMap(folder, file.Filename, f, contentType, file.Size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload to S3", "details": err.Error()})
		return
	}

	// 2. Update User Profile Picture via UserService (store Media ID)
	if err := h.Service.UpdateProfilePicture(userID, media.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user profile"})
		return
	}

	// Populate the presigned URL for the response
	h.MediaService.PopulateURL(media)

	c.JSON(http.StatusOK, gin.H{
		"id":  media.ID,
		"url": media.URL,
	})
}
