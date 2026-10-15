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
	}
}
