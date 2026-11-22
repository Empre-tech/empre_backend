package services

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"empre_backend/internal/models"
)

// Planes (2026-11-17, tablero «Base y Empre Pro»): un negocio es Pro si su suscripción está activa y no ha vencido
// (la misma que se paga con Wompi en Mi negocio > Suscripción). Lo de Pro se cierra también aquí, no solo en la app:
// la app lo muestra en gris con la corona, y si algo llega igual, el servidor responde ErrProRequired.

// ErrProRequired: eso es de Empre Pro y el negocio está en Base.
var ErrProRequired = errors.New("pro_required")

// subscriptionIsPro dice si una suscripción cuenta como Pro en este momento.
func subscriptionIsPro(sub models.Subscription, now time.Time) bool {
	return sub.Status == models.SubscriptionActive && sub.CurrentPeriodEnd != nil && sub.CurrentPeriodEnd.After(now)
}

// IsPro: el negocio tiene Empre Pro activo. Sin fila de suscripción, es Base.
func (s *LedgerService) IsPro(entityID uuid.UUID) (bool, error) {
	var sub models.Subscription
	res := s.db.Where("entity_id = ?", entityID).Limit(1).Find(&sub)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil
	}
	return subscriptionIsPro(sub, time.Now()), nil
}

// SetPro pone un negocio en Empre Pro hasta `until` (para pruebas, desde /api/admin/entities/:id/pro). Con until nil
// lo deja en Base. Usa la misma suscripción que se paga con Wompi: la app la ve igual que una pagada.
func (s *LedgerService) SetPro(entityID uuid.UUID, until *time.Time) (models.Subscription, error) {
	var ent models.Entity
	if err := s.db.Select("id").First(&ent, "id = ?", entityID).Error; err != nil {
		return models.Subscription{}, ErrLedgerNotFound
	}
	sub := models.Subscription{EntityID: entityID, Status: models.SubscriptionInactive}
	if until != nil {
		sub.Status, sub.CurrentPeriodEnd = models.SubscriptionActive, until
	}
	err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "entity_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "current_period_end", "updated_at"}),
	}).Create(&sub).Error
	if err != nil {
		return models.Subscription{}, err
	}
	err = s.db.First(&sub, "entity_id = ?", entityID).Error
	return sub, err
}

// exactAllowed: llevar la cantidad exacta (contar cuántos hay) es de Empre Pro; en Base se lleva el semáforo.
func (s *LedgerService) exactAllowed(entityID uuid.UUID) (bool, error) {
	if !s.PlansEnforced {
		return true, nil
	}
	return s.IsPro(entityID)
}

// requirePro cierra lo que es de Pro cuando los planes están activos (EMPRE_PLANS=off los apaga, para desarrollo).
func (s *LedgerService) requirePro(entityID uuid.UUID) error {
	if !s.PlansEnforced {
		return nil
	}
	pro, err := s.IsPro(entityID)
	if err != nil {
		return err
	}
	if !pro {
		return ErrProRequired
	}
	return nil
}
