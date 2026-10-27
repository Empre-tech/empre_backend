package handlers

import (
	"errors"
	"net/http"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type CatalogHandler struct {
	Service *services.CatalogService
}

func NewCatalogHandler(service *services.CatalogService) *CatalogHandler {
	return &CatalogHandler{Service: service}
}

type serviceItemDTO struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Section        string     `json:"section"`
	Price          *int       `json:"price"`
	PriceFrom      bool       `json:"price_from"`
	PriceUpdatedAt *time.Time `json:"price_updated_at"`
	ConfirmedAt    *time.Time `json:"confirmed_at"`
	// Fresh: el precio se fijó o confirmó hace poco. Sin fecha o viejo, la app lo marca «sin confirmar».
	Fresh bool `json:"fresh"`
	// Agenda (D2): cuánto dura y si se puede reservar por Empre. «Pregúntale» y la hoja de reservar los usan.
	DurationMin *int `json:"duration_min"`
	Bookable    bool `json:"bookable"`
	// SoldOut: se agotó (lo marca el inventario). La app lo muestra como «Agotado».
	SoldOut bool `json:"sold_out"`
	// Kind: service o product. KindChecked: el dueño ya lo confirmó (falso en ítems clasificados por la migración).
	Kind        string `json:"kind"`
	KindChecked bool   `json:"kind_checked"`
	// Variantes: ParentID/VariantLabel en una variante; PriceInherited = su precio es el del producto.
	ParentID       *uuid.UUID `json:"parent_id,omitempty"`
	VariantLabel   string     `json:"variant_label,omitempty"`
	PriceInherited bool       `json:"price_inherited,omitempty"`
}

func toServiceDTO(it models.ServiceItem, now time.Time) serviceItemDTO {
	return serviceItemDTO{
		ID: it.ID, Name: it.Name, Description: it.Description, Section: it.Section, Price: it.Price, PriceFrom: it.PriceFrom,
		PriceUpdatedAt: it.PriceUpdatedAt, ConfirmedAt: it.ConfirmedAt, Fresh: services.PriceFreshness(it, now),
		DurationMin: it.DurationMin, Bookable: it.Bookable, SoldOut: it.SoldOut,
		Kind: itemKind(it), KindChecked: it.KindChecked, ParentID: it.ParentID, VariantLabel: it.VariantLabel,
	}
}

// itemKind siempre responde service o product (un ítem viejo sin tipo se juzga por su duración).
func itemKind(it models.ServiceItem) string {
	if it.IsService() {
		return models.ItemService
	}
	return models.ItemProduct
}

// catalogError traduce un error del servicio a su código HTTP.
func catalogError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrCatalogNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "Negocio o servicio no encontrado"})
	case errors.Is(err, services.ErrCatalogNotOwner):
		c.JSON(http.StatusForbidden, gin.H{"error": "No autorizado"})
	case errors.Is(err, services.ErrCatalogLimit):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrCatalogInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "No se pudo completar la operación"})
	}
}

// List devuelve el catálogo activo de un negocio. Público.
// @Router /api/entities/{id}/catalog [get]
func (h *CatalogHandler) List(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	items, err := h.Service.List(entityID)
	if err != nil {
		catalogError(c, err)
		return
	}
	now := time.Now()
	parents := make(map[uuid.UUID]models.ServiceItem, len(items))
	for _, it := range items {
		parents[it.ID] = it
	}
	out := make([]serviceItemDTO, 0, len(items))
	for _, it := range items {
		d := toServiceDTO(it, now)
		// Una variante sin precio propio muestra el del producto.
		if it.ParentID != nil && it.Price == nil {
			if p, ok := parents[*it.ParentID]; ok && p.Price != nil {
				d.Price, d.PriceFrom, d.PriceInherited = p.Price, p.PriceFrom, true
			}
		}
		out = append(out, d)
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "fresh_days": services.CatalogFreshDays})
}

// Create añade un servicio (solo el dueño).
// @Router /api/entities/{id}/catalog [post]
func (h *CatalogHandler) Create(c *gin.Context) {
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
	var in services.ServiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cuerpo inválido"})
		return
	}
	now := time.Now()
	item, err := h.Service.Create(entityID, userID, in, now)
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toServiceDTO(*item, now))
}

// Update edita un servicio (solo el dueño).
// @Router /api/entities/{id}/catalog/{itemId} [put]
func (h *CatalogHandler) Update(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	itemID, err2 := uuid.Parse(c.Param("itemId"))
	if err != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}
	var in services.ServiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cuerpo inválido"})
		return
	}
	now := time.Now()
	item, err := h.Service.Update(entityID, itemID, userID, in, now)
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toServiceDTO(*item, now))
}

// Retire retira un servicio del catálogo (solo el dueño): deja de mostrarse.
// @Router /api/entities/{id}/catalog/{itemId} [delete]
func (h *CatalogHandler) Retire(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	itemID, err2 := uuid.Parse(c.Param("itemId"))
	if err != nil || err2 != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID inválido"})
		return
	}
	userID, ok := ownerID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "No autenticado"})
		return
	}
	if err := h.Service.Retire(entityID, itemID, userID); err != nil {
		catalogError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Confirm da por vigente todo el catálogo hoy (solo el dueño).
// @Router /api/entities/{id}/catalog/confirm [post]
func (h *CatalogHandler) Confirm(c *gin.Context) {
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
	n, err := h.Service.Confirm(entityID, userID, time.Now())
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"confirmed": n})
}

// SetKind cambia un ítem entre servicio y producto (solo el dueño).
// @Router /api/entities/{id}/catalog/{itemId}/kind [put]
func (h *CatalogHandler) SetKind(c *gin.Context) {
	entityID, err := uuid.Parse(c.Param("id"))
	itemID, err2 := uuid.Parse(c.Param("itemId"))
	if err != nil || err2 != nil {
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
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cuerpo inválido"})
		return
	}
	item, err := h.Service.SetKind(entityID, itemID, userID, in.Kind)
	if err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, toServiceDTO(*item, time.Now()))
}

// ConfirmKinds da por revisado qué es cada ítem (solo el dueño).
// @Router /api/entities/{id}/catalog/kinds/confirm [post]
func (h *CatalogHandler) ConfirmKinds(c *gin.Context) {
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
	if err := h.Service.ConfirmKinds(entityID, userID); err != nil {
		catalogError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
