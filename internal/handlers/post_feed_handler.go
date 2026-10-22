package handlers

import (
	"net/http"
	"strconv"

	"empre_backend/internal/dtos"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PostFeedHandler struct {
	Service *services.PostFeedService
}

func NewPostFeedHandler(service *services.PostFeedService) *PostFeedHandler {
	return &PostFeedHandler{Service: service}
}

// List devuelve las publicaciones (fotos y videos) de un negocio por páginas, las más recientes primero. Público.
// `type`: all (por defecto), photo o video. Hasta 50 por página.
// @Router /api/entities/{id}/posts [get]
func (h *PostFeedHandler) List(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(services.PostPageDefault)))
	if pageSize <= 0 {
		pageSize = services.PostPageDefault
	}
	if pageSize > services.PostPageMax {
		pageSize = services.PostPageMax
	}
	kind := c.DefaultQuery("type", services.PostKindAll)
	if kind != services.PostKindPhoto && kind != services.PostKindVideo {
		kind = services.PostKindAll
	}
	posts, total, err := h.Service.Page(entityID, page, pageSize, kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudieron cargar las publicaciones"})
		return
	}
	out := make([]dtos.PostResponse, 0, len(posts))
	for _, p := range posts {
		items := make([]dtos.PostItemResponse, 0, len(p.Photos))
		for _, ph := range p.Photos {
			items = append(items, dtos.PostItemResponse{ID: ph.ID, URL: ph.Media.URL, ContentType: ph.Media.ContentType})
		}
		out = append(out, dtos.PostResponse{ID: p.ID, Caption: p.Caption, CreatedAt: p.CreatedAt, Items: items})
	}
	c.JSON(http.StatusOK, gin.H{"data": out, "meta": gin.H{"total": total, "page": page, "page_size": pageSize}})
}
