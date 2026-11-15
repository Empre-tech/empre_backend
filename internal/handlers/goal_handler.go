package handlers

import (
	"errors"
	"net/http"
	"regexp"

	"empre_backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GoalHandler: la meta del mes de un negocio (tablero «Actividad y meta»). Solo la ve y la cambia el dueño.
type GoalHandler struct {
	DB *gorm.DB
}

var goalMonth = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// GoalKinds son las metas que se pueden poner: lo que vendes o lo que te queda.
var GoalKinds = map[string]bool{"sales": true, "left": true}

// ValidGoal dice si una meta tiene sentido: mes «AAAA-MM», un tipo conocido y una cifra positiva y razonable.
func ValidGoal(month, kind string, target int64) bool {
	return goalMonth.MatchString(month) && GoalKinds[kind] && target > 0 && target <= 100_000_000_000
}

func (h *GoalHandler) owned(c *gin.Context) (uuid.UUID, bool) {
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return uuid.Nil, false
	}
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return uuid.Nil, false
	}
	var e models.Entity
	if err := h.DB.Select("id", "owner_id").First(&e, "id = ?", entityID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio no encontrado"})
		return uuid.Nil, false
	}
	if e.OwnerID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Solo el dueño puede ver su meta"})
		return uuid.Nil, false
	}
	return entityID, true
}

// Get devuelve la meta de un mes (?month=AAAA-MM), o null si no hay.
// @Router /api/entities/{id}/goal [get]
func (h *GoalHandler) Get(c *gin.Context) {
	entityID, ok := h.owned(c)
	if !ok {
		return
	}
	month := c.Query("month")
	if !goalMonth.MatchString(month) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mes inválido"})
		return
	}
	var g models.EntityGoal
	err := h.DB.Where("entity_id = ? AND month = ?", entityID, month).First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"goal": nil})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos cargar tu meta"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"goal": g})
}

type goalInput struct {
	Month  string `json:"month" binding:"required"`
	Kind   string `json:"kind" binding:"required"`
	Target int64  `json:"target" binding:"required"`
}

// Put pone o cambia la meta del mes (una sola por mes).
// @Router /api/entities/{id}/goal [put]
func (h *GoalHandler) Put(c *gin.Context) {
	entityID, ok := h.owned(c)
	if !ok {
		return
	}
	var in goalInput
	if err := c.ShouldBindJSON(&in); err != nil || !ValidGoal(in.Month, in.Kind, in.Target) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "La meta necesita un mes, un tipo y una cifra mayor que cero"})
		return
	}
	g := models.EntityGoal{EntityID: entityID, Month: in.Month, Kind: in.Kind, Target: in.Target}
	err := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entity_id"}, {Name: "month"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "target", "updated_at"}),
	}).Create(&g).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos guardar tu meta"})
		return
	}
	var saved models.EntityGoal
	h.DB.Where("entity_id = ? AND month = ?", entityID, in.Month).First(&saved)
	c.JSON(http.StatusOK, gin.H{"goal": saved})
}

// Delete quita la meta de un mes (?month=AAAA-MM).
// @Router /api/entities/{id}/goal [delete]
func (h *GoalHandler) Delete(c *gin.Context) {
	entityID, ok := h.owned(c)
	if !ok {
		return
	}
	month := c.Query("month")
	if !goalMonth.MatchString(month) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mes inválido"})
		return
	}
	if err := h.DB.Where("entity_id = ? AND month = ?", entityID, month).Delete(&models.EntityGoal{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No pudimos quitar tu meta"})
		return
	}
	c.Status(http.StatusNoContent)
}
