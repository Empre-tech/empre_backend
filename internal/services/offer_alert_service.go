package services

import (
	"errors"
	"fmt"
	"log"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AlertCooldown es lo mínimo entre dos avisos del mismo «Avísame»: si en una tarde publican
// cinco ofertas, la persona recibe uno, no cinco.
const AlertCooldown = 12 * time.Hour

// OfferAlertService guarda los «Avísame» de Ofertas y avisa cuando sale una oferta que coincide.
type OfferAlertService struct {
	db   *gorm.DB
	push *PushService
}

func NewOfferAlertService(db *gorm.DB, push *PushService) *OfferAlertService {
	return &OfferAlertService{db: db, push: push}
}

// AlertView es un aviso con los nombres que la app muestra.
type AlertView struct {
	models.OfferAlert
	ZoneName     string `json:"zone_name"`
	CategoryName string `json:"category_name,omitempty"`
}

var ErrZoneNotFound = errors.New("esa zona no existe")

// Create guarda el aviso; si ya existía, devuelve el mismo (pedirlo dos veces no duplica).
func (s *OfferAlertService) Create(userID, zoneID uuid.UUID, categoryID *uuid.UUID) (*models.OfferAlert, error) {
	var n int64
	if err := s.db.Model(&models.Zone{}).Where("id = ? AND active", zoneID).Count(&n).Error; err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrZoneNotFound
	}
	var a models.OfferAlert
	q := s.db.Where("user_id = ? AND zone_id = ?", userID, zoneID)
	if categoryID == nil {
		q = q.Where("category_id IS NULL")
	} else {
		q = q.Where("category_id = ?", *categoryID)
	}
	err := q.First(&a).Error
	if err == nil {
		return &a, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	a = models.OfferAlert{UserID: userID, ZoneID: zoneID, CategoryID: categoryID}
	if err := s.db.Create(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// Remove borra un aviso de la persona; si no es suyo, no pasa nada.
func (s *OfferAlertService) Remove(userID, id uuid.UUID) error {
	return s.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.OfferAlert{}).Error
}

// Mine devuelve los avisos de la persona, con el nombre de la zona y la categoría.
func (s *OfferAlertService) Mine(userID uuid.UUID) ([]AlertView, error) {
	var out []AlertView
	err := s.db.Table("offer_alerts a").
		Select("a.*, z.name AS zone_name, COALESCE(c.name, '') AS category_name").
		Joins("JOIN zones z ON z.id = a.zone_id").
		Joins("LEFT JOIN categories c ON c.id = a.category_id").
		Where("a.user_id = ?", userID).
		Order("a.created_at DESC").
		Scan(&out).Error
	return out, err
}

// AlertDue dice si a un aviso le toca sonar: nunca ha sonado o ya pasó el descanso.
func AlertDue(last *time.Time, now time.Time) bool {
	return last == nil || now.Sub(*last) >= AlertCooldown
}

// AlertMatches dice si el aviso cubre la oferta: sin categoría cubre todas; con categoría, la
// de la oferta, la del negocio o la madre de cualquiera de ellas.
func AlertMatches(alertCat *uuid.UUID, offerCats map[uuid.UUID]bool) bool {
	return alertCat == nil || offerCats[*alertCat]
}

// AlertText es lo que dice la notificación.
func AlertText(zone, entity, title string) (string, string) {
	return fmt.Sprintf("Nueva oferta en %s", zone), fmt.Sprintf("%s: %s", entity, title)
}

// NotifyFor avisa a quien pidió «Avísame» en la zona del negocio de la oferta. Se llama al
// publicar; los errores se anotan y no detienen la publicación.
func (s *OfferAlertService) NotifyFor(offerID uuid.UUID) {
	if s == nil || s.push == nil {
		return
	}
	var offer models.Offer
	if err := s.db.Preload("Entity").First(&offer, "id = ?", offerID).Error; err != nil {
		log.Printf("avisos de ofertas: no se pudo leer la oferta %s: %v", offerID, err)
		return
	}
	e := offer.Entity
	if e.ZoneID == nil {
		return
	}
	var zone models.Zone
	if err := s.db.First(&zone, "id = ?", *e.ZoneID).Error; err != nil {
		return
	}

	cats := map[uuid.UUID]bool{e.CategoryID: true}
	if offer.CategoryID != nil {
		cats[*offer.CategoryID] = true
	}
	ids := make([]uuid.UUID, 0, len(cats))
	for id := range cats {
		ids = append(ids, id)
	}
	var parents []uuid.UUID
	s.db.Model(&models.Category{}).Where("id IN ? AND parent_id IS NOT NULL", ids).Pluck("parent_id", &parents)
	for _, p := range parents {
		cats[p] = true
	}

	var alerts []models.OfferAlert
	if err := s.db.Where("zone_id = ? AND user_id <> ?", zone.ID, e.OwnerID).Find(&alerts).Error; err != nil {
		log.Printf("avisos de ofertas: no se pudieron leer los avisos: %v", err)
		return
	}
	now := time.Now()
	title, body := AlertText(zone.Name, e.Name, offer.Title)
	sent := map[uuid.UUID]bool{}
	for _, a := range alerts {
		if sent[a.UserID] || !AlertMatches(a.CategoryID, cats) || !AlertDue(a.LastSentAt, now) {
			continue
		}
		sent[a.UserID] = true
		s.push.Notify(a.UserID, title, body, map[string]string{"type": "offer", "offer_id": offer.ID.String(), "entity_id": e.ID.String()})
		s.db.Model(&models.OfferAlert{}).Where("id = ?", a.ID).Update("last_sent_at", now)
	}
}
