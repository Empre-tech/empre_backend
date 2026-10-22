package services

import (
	"errors"
	"strings"
	"unicode/utf8"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// «Haz que tu negocio destaque» (3.40, mockup vitrina_viva.html): los 8 puntos que cambian lo que el cliente ve del
// negocio. Cada uno se calcula con datos que ya existen; los textos (qué hacer y qué logra) viven en la app. Completar
// el perfil no cambia el orden en ninguna lista: ningún negocio va por encima de otro.

const (
	CompletenessMinPhotos = 3  // «3 fotos de tu negocio»
	CompletenessMinDesc   = 40 // una descripción de menos letras no le dice nada a quien abre el perfil
)

var ErrNotOwner = errors.New("solo el dueño puede ver esto")

// CompletenessInput son los datos del negocio que miden los puntos.
type CompletenessInput struct {
	HasProfile  bool
	HasCover    bool
	Photos      int
	HasHours    bool
	Tagline     string
	Description string
	PricedItems int // servicios activos con precio
	OffersEver  int // ofertas publicadas alguna vez (no «activa»: así la lista no vuelve cada vez que una vence)
}

type CompletenessItem struct {
	Key   string `json:"key"`
	Group string `json:"group"` // look | found | sell
	Done  bool   `json:"done"`
}

// BuildCompleteness devuelve los 8 puntos en el orden en que se muestran.
func BuildCompleteness(in CompletenessInput) []CompletenessItem {
	return []CompletenessItem{
		{"photo", "look", in.HasProfile},
		{"cover", "look", in.HasCover},
		{"photos", "look", in.Photos >= CompletenessMinPhotos},
		{"hours", "found", in.HasHours},
		{"tagline", "found", strings.TrimSpace(in.Tagline) != ""},
		{"description", "found", utf8.RuneCountInString(strings.TrimSpace(in.Description)) >= CompletenessMinDesc},
		{"catalog", "sell", in.PricedItems > 0},
		{"offer", "sell", in.OffersEver > 0},
	}
}

type CompletenessService struct{ db *gorm.DB }

func NewCompletenessService(db *gorm.DB) *CompletenessService { return &CompletenessService{db: db} }

// For calcula los puntos de un negocio; solo para su dueño.
func (s *CompletenessService) For(entityID, userID uuid.UUID) ([]CompletenessItem, error) {
	var e models.Entity
	if err := s.db.Select("id", "owner_id", "profile_media_id", "banner_media_id", "tagline", "description").First(&e, "id = ?", entityID).Error; err != nil {
		return nil, err
	}
	if e.OwnerID != userID {
		return nil, ErrNotOwner
	}
	in := CompletenessInput{HasProfile: e.ProfileMediaID != nil, HasCover: e.BannerMediaID != nil, Tagline: e.Tagline, Description: e.Description}
	var n int64
	if err := s.db.Model(&models.EntityPhoto{}).Where("entity_id = ?", entityID).Count(&n).Error; err != nil {
		return nil, err
	}
	in.Photos = int(n)
	if err := s.db.Model(&models.BusinessHour{}).Where("entity_id = ?", entityID).Count(&n).Error; err != nil {
		return nil, err
	}
	in.HasHours = n > 0
	if err := s.db.Model(&models.ServiceItem{}).Where("entity_id = ? AND status = ? AND price IS NOT NULL", entityID, models.ServiceActive).Count(&n).Error; err != nil {
		return nil, err
	}
	in.PricedItems = int(n)
	// Unscoped: una oferta borrada también cuenta como «ya publicaste una».
	if err := s.db.Unscoped().Model(&models.Offer{}).Where("entity_id = ?", entityID).Count(&n).Error; err != nil {
		return nil, err
	}
	in.OffersEver = int(n)
	return BuildCompleteness(in), nil
}
