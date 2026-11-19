package services

import (
	"time"

	"github.com/google/uuid"

	"empre_backend/internal/models"
)

// Redactar con Empre AI (decisión del usuario, 2026-11-17): es de Empre Pro y cuenta en el mismo tope del mes que
// «Habla con Empre AI», salvo la primera descripción al crear el negocio. Esa va sin negocio todavía, es gratis y
// tiene un tope diario por persona para que no sirva de puerta trasera.

// FreeCreateWritesPerDay: pedidos gratis por persona y día para la descripción del negocio que se está creando.
const FreeCreateWritesPerDay = 10

// writingNeedsBusiness dice si ese tipo de texto solo se pide para un negocio que ya existe (y entonces es de Pro).
// La descripción puede pedirse sin negocio: es la del negocio que se está creando.
func writingNeedsBusiness(kind string) bool {
	return kind != "business_description"
}

// aiUsedThisMonth: lo que el negocio lleva de Empre AI este mes, dictados y textos juntos.
func (s *LedgerService) aiUsedThisMonth(entityID uuid.UUID) (int64, error) {
	monthStart, _, _ := PeriodRange("month", nowCO())
	var drafts, writes int64
	if err := s.db.Model(&models.VoiceDraft{}).Where("entity_id = ? AND created_at >= ?", entityID, monthStart).Count(&drafts).Error; err != nil {
		return 0, err
	}
	if err := s.db.Model(&models.AIWrite{}).Where("entity_id = ? AND created_at >= ?", entityID, monthStart).Count(&writes).Error; err != nil {
		return 0, err
	}
	return drafts + writes, nil
}

// CheckWriting decide si se puede pedir un texto. Con negocio: tiene que ser del dueño, estar en Pro y tener tope.
// Sin negocio: solo la descripción del que se está creando, con su tope diario por persona.
func (s *LedgerService) CheckWriting(userID uuid.UUID, entityID *uuid.UUID, kind string) error {
	if entityID == nil {
		if writingNeedsBusiness(kind) {
			return &InvalidError{"ese texto se redacta desde tu negocio"}
		}
		var today int64
		if err := s.db.Model(&models.AIWrite{}).Where("user_id = ? AND entity_id IS NULL AND created_at >= ?", userID, time.Now().Add(-24*time.Hour)).Count(&today).Error; err != nil {
			return err
		}
		if today >= FreeCreateWritesPerDay {
			return ErrVoiceLimit
		}
		return nil
	}
	if err := s.requireOwner(userID, *entityID); err != nil {
		return err
	}
	if err := s.requirePro(*entityID); err != nil {
		return err
	}
	used, err := s.aiUsedThisMonth(*entityID)
	if err != nil {
		return err
	}
	if s.VoiceMonthlyLimit > 0 && int(used) >= s.VoiceMonthlyLimit {
		return ErrVoiceLimit
	}
	return nil
}

// RecordWriting anota un pedido que sí entregó opciones (los que fallan no cuentan).
func (s *LedgerService) RecordWriting(userID uuid.UUID, entityID *uuid.UUID, kind string) error {
	return s.db.Create(&models.AIWrite{ID: uuid.New(), EntityID: entityID, UserID: userID, Kind: kind, CreatedAt: time.Now()}).Error
}
