package handlers

import (
	"net/http"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StatusHandler manages a business's real-time availability.
type StatusHandler struct {
	DB *gorm.DB
}

func NewStatusHandler(db *gorm.DB) *StatusHandler {
	return &StatusHandler{DB: db}
}

type UpdateStatusRequest struct {
	Availability string `json:"availability"`
}

// Update sets the business availability (owner only).
// @Router /api/entities/{id}/status [patch]
func (h *StatusHandler) Update(c *gin.Context) {
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

	var entity models.Entity
	if err := h.DB.First(&entity, "id = ?", entityID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio no encontrado"})
		return
	}
	if entity.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "No autorizado"})
		return
	}

	var req UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var status models.BusinessStatus
	err = h.DB.Where("entity_id = ?", entityID).First(&status).Error
	switch {
	case err == gorm.ErrRecordNotFound:
		status = models.BusinessStatus{
			EntityID:     entityID,
			Availability: req.Availability,
			UpdatedAt:    utils.NowInCartagena(),
		}
		if err := h.DB.Create(&status).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case err == nil:
		status.Availability = req.Availability
		status.UpdatedAt = utils.NowInCartagena()
		if err := h.DB.Save(&status).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Pasadas 6 h sin actualizar, la disponibilidad se oculta (queda solo abierto/cerrado).
	c.JSON(http.StatusOK, gin.H{"availability": status.Current(utils.NowInCartagena()), "updated_at": status.UpdatedAt})
}

// Get returns the business availability (public).
// @Router /api/entities/{id}/status [get]
func (h *StatusHandler) Get(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}

	var status models.BusinessStatus
	if err := h.DB.Where("entity_id = ?", entityID).First(&status).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"availability": "", "updated_at": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{"availability": status.Availability, "updated_at": status.UpdatedAt})
}
