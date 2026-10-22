package handlers

import (
	"net/http"
	"strings"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ReviewSnippetHandler struct {
	Service *services.ReviewSnippetService
}

func NewReviewSnippetHandler(service *services.ReviewSnippetService) *ReviewSnippetHandler {
	return &ReviewSnippetHandler{Service: service}
}

// List devuelve la frase de reseña de cada negocio pedido (hasta SnippetMaxIDs). Público.
// @Router /api/discovery/review-snippets [get]
func (h *ReviewSnippetHandler) List(c *gin.Context) {
	var ids []uuid.UUID
	for _, part := range strings.Split(c.Query("ids"), ",") {
		if id, err := uuid.Parse(strings.TrimSpace(part)); err == nil {
			ids = append(ids, id)
		}
	}
	got, err := h.Service.Latest(ids, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudieron cargar las reseñas"})
		return
	}
	out := make(map[string]dtos.ReviewSnippetResponse, len(got))
	for id, s := range got {
		out[id.String()] = dtos.ReviewSnippetResponse{Text: s.Comment, Rating: s.Rating, CreatedAt: s.CreatedAt, Author: dtos.ShortName(s.AuthorName), Verified: true}
	}
	c.JSON(http.StatusOK, gin.H{"snippets": out})
}
