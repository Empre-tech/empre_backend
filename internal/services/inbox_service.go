package services

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Resumen de lo pendiente para las insignias de la app (3.37, avisos_negocio.html): conversaciones sin leer como
// cliente y, por cada negocio propio, conversaciones sin leer y solicitudes de cita por revisar. Tres consultas
// agrupadas, sin importar cuántos negocios o mensajes haya.

type InboxBusiness struct {
	EntityID            uuid.UUID `json:"entity_id"`
	Name                string    `json:"name"`
	UnreadConversations int       `json:"unread_conversations"`
	PendingRequests     int       `json:"pending_requests"`
}

type InboxSummary struct {
	PersonalUnread int             `json:"personal_unread"`
	Businesses     []InboxBusiness `json:"businesses"`
}

type InboxService struct{ db *gorm.DB }

func NewInboxService(db *gorm.DB) *InboxService { return &InboxService{db: db} }

func (s *InboxService) Summary(viewerID uuid.UUID, now time.Time) (InboxSummary, error) {
	out := InboxSummary{Businesses: []InboxBusiness{}}

	var owned []models.Entity
	if err := s.db.Select("id", "name").Where("owner_id = ?", viewerID).Order("name ASC").Find(&owned).Error; err != nil {
		return out, err
	}
	idx := map[uuid.UUID]int{}
	for i, e := range owned {
		idx[e.ID] = i
		out.Businesses = append(out.Businesses, InboxBusiness{EntityID: e.ID, Name: e.Name})
	}

	// Conversaciones con algo sin leer, del lado de quien mira (la misma regla que la lista de conversaciones).
	type convRow struct {
		EntityID uuid.UUID
		AsOwner  bool
	}
	var convs []convRow
	if err := s.db.Raw(`
		SELECT m.entity_id AS entity_id, (e.owner_id = ?) AS as_owner
		FROM messages m
		JOIN entities e ON m.entity_id = e.id
		WHERE m.deleted_at IS NULL AND m.is_read = false
		  AND ((e.owner_id = ? AND m.sent_by_entity = false) OR (m.user_id = ? AND m.sent_by_entity = true))
		GROUP BY m.entity_id, m.user_id, e.owner_id`, viewerID, viewerID, viewerID).Scan(&convs).Error; err != nil {
		return out, err
	}
	for _, c := range convs {
		if !c.AsOwner {
			out.PersonalUnread++
		} else if i, ok := idx[c.EntityID]; ok {
			out.Businesses[i].UnreadConversations++
		}
	}

	if len(owned) > 0 {
		type reqRow struct {
			EntityID uuid.UUID
			N        int
		}
		var reqs []reqRow
		if err := s.db.Model(&models.Appointment{}).
			Select("entity_id, count(*) AS n").
			Where("entity_id IN (SELECT id FROM entities WHERE owner_id = ?) AND kind = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)",
				viewerID, models.AppointmentBooking, models.ApptRequested, now).
			Group("entity_id").Scan(&reqs).Error; err != nil {
			return out, err
		}
		for _, r := range reqs {
			if i, ok := idx[r.EntityID]; ok {
				out.Businesses[i].PendingRequests = r.N
			}
		}
	}
	return out, nil
}
