package handlers

import (
	"errors"
	"net/http"

	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SectionHandler: secciones del catálogo (lo que vendes e insumos). El dueño elige de un catálogo global; no crea.
type SectionHandler struct {
	Service *services.SectionService
}

func NewSectionHandler(s *services.SectionService) *SectionHandler {
	return &SectionHandler{Service: s}
}

func sectionError(c *gin.Context, err error) {
	var inv *services.InvalidError
	switch {
	case errors.As(err, &inv):
		c.JSON(http.StatusBadRequest, gin.H{"error": inv.Msg})
	case errors.Is(err, services.ErrSectionNotFound), errors.Is(err, services.ErrCatalogNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "No encontrado"})
	case errors.Is(err, services.ErrCatalogNotOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": "No autorizado"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudo completar la operación"})
	}
}

// ForEntity: las secciones del negocio, con cuántos ítems tiene cada una. ?kind=sell|supply. Solo el dueño.
// @Router /api/entities/{id}/sections [get]
func (h *SectionHandler) ForEntity(c *gin.Context) {
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
	out, err := h.Service.ForEntity(userID, entityID, c.DefaultQuery("kind", "sell"))
	if err != nil {
		sectionError(c, err)
		return
	}
	// audience: si el negocio dice para quién es cada producto (ropa, calzado, deporte).
	c.JSON(http.StatusOK, gin.H{"sections": out, "audience": services.AudienceEnabled(h.Service.DB(), entityID)})
}

// Add: agrega una sección del catálogo a las del negocio. Body {section_id}.
// @Router /api/entities/{id}/sections [post]
func (h *SectionHandler) Add(c *gin.Context) {
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
	var in struct {
		SectionID uuid.UUID `json:"section_id"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.SectionID == uuid.Nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Falta section_id"})
		return
	}
	out, err := h.Service.Add(userID, entityID, in.SectionID)
	if err != nil {
		sectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"section": out})
}

// Request: el negocio pide una sección que no existe. Body {kind, name}. Si ya existe con ese nombre, se la agrega
// de una vez y responde {section}; si no, abre la petición y responde {request}.
// @Router /api/entities/{id}/sections/requests [post]
func (h *SectionHandler) Request(c *gin.Context) {
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
	var in struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cuerpo inválido"})
		return
	}
	req, sec, err := h.Service.Request(userID, entityID, in.Kind, in.Name)
	if err != nil {
		sectionError(c, err)
		return
	}
	if sec != nil {
		c.JSON(http.StatusOK, gin.H{"section": sec})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"request": req})
}

// Catalog: el catálogo global de secciones de un tipo. ?kind=sell|supply&q=texto. Requiere sesión.
// @Router /api/sections [get]
func (h *SectionHandler) Catalog(c *gin.Context) {
	out, err := h.Service.Catalog(c.DefaultQuery("kind", "sell"), c.Query("q"))
	if err != nil {
		sectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sections": out})
}

// ListRequests: peticiones de secciones (?status=open|added|dismissed, por defecto open). Solo admin.
// @Router /api/admin/section-requests [get]
func (h *SectionHandler) ListRequests(c *gin.Context) {
	out, err := h.Service.ListRequests(c.Query("status"))
	if err != nil {
		sectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"requests": out})
}

// ResolveRequest: {action: "add"|"dismiss", name?}. «add» crea la sección (con name si se cambia el nombre pedido)
// y se la deja puesta al negocio que la pidió. Solo admin.
// @Router /api/admin/section-requests/{requestId} [post]
func (h *SectionHandler) ResolveRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("requestId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	var in struct {
		Action string `json:"action"`
		Name   string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || (in.Action != "add" && in.Action != "dismiss") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action debe ser add o dismiss"})
		return
	}
	req, err := h.Service.ResolveRequest(id, in.Action == "add", in.Name)
	if err != nil {
		sectionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": req})
}
