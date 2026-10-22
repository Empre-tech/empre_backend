package dtos

import (
	"strings"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// PromotionResponse es la forma pública de una promoción. `tags` va como array.
type PromotionResponse struct {
	ID          uuid.UUID `json:"id"`
	EntityID    uuid.UUID `json:"entity_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Price       string    `json:"price"`
	Discount    string    `json:"discount"`
	NormalPrice string    `json:"normal_price"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Conditions  string    `json:"conditions"`
	Tags        []string  `json:"tags"`
	Status      string    `json:"status"`
	// Editor por plantillas: mensaje para clientes, plantilla, portada y servicios destacados (copia congelada).
	PublicMessage string     `json:"public_message,omitempty"`
	Template      string     `json:"template,omitempty"`
	PhotoMediaID  *uuid.UUID `json:"photo_media_id,omitempty"`
	// PhotoURL es la portada lista para mostrar: la ruta pública del medio (/api/images/{id}), que redirige a un enlace
	// nuevo cada vez (no caduca como una URL prefirmada guardada). Vacía si no hay portada.
	PhotoURL string                  `json:"photo_url,omitempty"`
	Items    []PromotionItemResponse `json:"items,omitempty"`
}

// PromotionItemResponse es un servicio que destaca la promoción, tal como estaba al congelarse.
type PromotionItemResponse struct {
	Name  string `json:"name"`
	Price *int   `json:"price,omitempty"`
}

// PromotionOwnerResponse es lo que ve el DUEÑO: lo público más el motivo interno de la campaña, que nunca sale en
// una respuesta pública.
type PromotionOwnerResponse struct {
	PromotionResponse
	MotiveInternal string `json:"motive_internal"`
}

// PromotionOwnerResponseFrom es la forma de una promoción para su dueño.
func PromotionOwnerResponseFrom(p *models.Promotion) PromotionOwnerResponse {
	return PromotionOwnerResponse{PromotionResponse: PromotionResponseFrom(p), MotiveInternal: p.MotiveInternal}
}

func splitTags(raw string) []string {
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func PromotionResponseFrom(p *models.Promotion) PromotionResponse {
	return PromotionResponse{
		ID:          p.ID,
		EntityID:    p.EntityID,
		Title:       p.Title,
		Description: p.Description,
		Price:       p.Price,
		Discount:    p.Discount,
		NormalPrice: p.NormalPrice,
		StartsAt:    p.StartsAt,
		EndsAt:      p.EndsAt,
		Conditions:  p.Conditions,
		Tags:        splitTags(p.Tags),
		Status:      string(p.Status),

		PublicMessage: p.PublicMessage,
		Template:      string(p.Template),
		PhotoMediaID:  p.PhotoMediaID,
		PhotoURL:      mediaPath(p.PhotoMediaID),
		Items:         promotionItems(p.Items),
	}
}

// mediaPath es la ruta pública de un medio, o vacía si no hay.
func mediaPath(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return "/api/images/" + id.String()
}

func promotionItems(items []models.PromotionItem) []PromotionItemResponse {
	if len(items) == 0 {
		return nil
	}
	out := make([]PromotionItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, PromotionItemResponse{Name: it.Name, Price: it.Price})
	}
	return out
}
