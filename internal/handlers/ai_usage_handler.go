package handlers

import (
	"net/http"
	"strconv"
	"time"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
)

// AIUsageHandler muestra al admin cuántos tokens y cuánta plata se va en IA, por función y por negocio.
type AIUsageHandler struct {
	Usage *services.AIUsageService
}

// Summary: GET /api/admin/ai-usage?days=30&top=20
func (h *AIUsageHandler) Summary(c *gin.Context) {
	days, err := strconv.Atoi(c.DefaultQuery("days", "30"))
	if err != nil || days < 1 || days > 366 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days debe estar entre 1 y 366"})
		return
	}
	top, err := strconv.Atoi(c.DefaultQuery("top", "20"))
	if err != nil || top < 1 || top > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "top debe estar entre 1 y 200"})
		return
	}
	out, err := h.Usage.Summary(days, top, time.Now())
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}
