package services

import (
	"errors"
	"log"
	"sort"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OfferWindow es cuánto tiene cada persona de la lista de espera para tomar una hora que se liberó; después pasa a la
// siguiente (tablero «Agenda del negocio»: «con 10 minutos para cada uno»).
const OfferWindow = 10 * time.Minute

// minLeadForOffer: una hora que se libera con menos de esto no se ofrece (no alcanzaría a llegar).
const minLeadForOffer = 20 * time.Minute

var ErrOfferGone = errors.New("offer_gone")

// DayKey es el día en Colombia de un instante, como lo guarda la lista de espera.
func DayKey(t time.Time) string {
	return t.In(nowCO().Location()).Format("2006-01-02")
}

// OfferExpired dice si ya se le acabó el tiempo a quien se le ofreció la hora.
func OfferExpired(offeredAt *time.Time, now time.Time) bool {
	return offeredAt == nil || now.Sub(*offeredAt) >= OfferWindow
}

// NextWaiting es el siguiente en la fila: el que llegó primero entre los que siguen esperando.
func NextWaiting(entries []models.WaitlistEntry) *models.WaitlistEntry {
	var waiting []models.WaitlistEntry
	for _, e := range entries {
		if e.Status == models.WaitWaiting {
			waiting = append(waiting, e)
		}
	}
	if len(waiting) == 0 {
		return nil
	}
	sort.SliceStable(waiting, func(i, j int) bool { return waiting[i].CreatedAt.Before(waiting[j].CreatedAt) })
	return &waiting[0]
}

// WaitlistView es una persona de la lista con su nombre, para el dueño y para quien espera.
type WaitlistView struct {
	models.WaitlistEntry
	Name       string `json:"name"`
	EntityName string `json:"entity_name,omitempty"`
}

// JoinWaitlist anota a la persona en la lista de espera del día. Si ya estaba (esperando o con oferta), devuelve esa.
func (s *AgendaService) JoinWaitlist(userID, entityID uuid.UUID, day time.Time, serviceID *uuid.UUID) (*models.WaitlistEntry, error) {
	e, err := s.entity(entityID)
	if err != nil {
		return nil, err
	}
	if e.OwnerID == userID {
		return nil, ErrOwnBusiness
	}
	if s.entityHidden(entityID) {
		return nil, ErrEntityHidden
	}
	key := DayKey(day)
	if key < DayKey(time.Now()) {
		return nil, &InvalidError{Msg: "Ese día ya pasó"}
	}
	var cur models.WaitlistEntry
	err = s.db.Where("entity_id = ? AND user_id = ? AND day = ? AND status IN ?", entityID, userID, key,
		[]models.WaitlistStatus{models.WaitWaiting, models.WaitOffered}).First(&cur).Error
	if err == nil {
		return &cur, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	w := models.WaitlistEntry{EntityID: entityID, UserID: userID, Day: key, ServiceID: serviceID, Status: models.WaitWaiting}
	if err := s.db.Create(&w).Error; err != nil {
		return nil, err
	}
	return &w, nil
}

// LeaveWaitlist saca a la persona de la lista.
func (s *AgendaService) LeaveWaitlist(userID, id uuid.UUID) error {
	return s.db.Model(&models.WaitlistEntry{}).
		Where("id = ? AND user_id = ? AND status IN ?", id, userID, []models.WaitlistStatus{models.WaitWaiting, models.WaitOffered}).
		Update("status", models.WaitLeft).Error
}

// MyWaitlist: las listas en que está la persona desde hoy, con el nombre del negocio.
func (s *AgendaService) MyWaitlist(userID uuid.UUID) ([]WaitlistView, error) {
	var out []WaitlistView
	err := s.db.Table("waitlist_entries w").
		Select("w.*, e.name AS entity_name").
		Joins("JOIN entities e ON e.id = w.entity_id").
		Where("w.user_id = ? AND w.day >= ? AND w.status IN ?", userID, DayKey(time.Now()),
			[]models.WaitlistStatus{models.WaitWaiting, models.WaitOffered, models.WaitBooked}).
		Order("w.day, w.created_at").Scan(&out).Error
	return out, err
}

// BusinessWaitlist: la lista de un día para el dueño, en orden de llegada, con quién tomó la hora y a quién se avisó.
func (s *AgendaService) BusinessWaitlist(ownerID, entityID uuid.UUID, day time.Time) ([]WaitlistView, error) {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var out []WaitlistView
	err := s.db.Table("waitlist_entries w").
		Select("w.*, COALESCE(u.name, '') AS name").
		Joins("LEFT JOIN users u ON u.id = w.user_id").
		Where("w.entity_id = ? AND w.day = ? AND w.status <> ?", entityID, DayKey(day), models.WaitLeft).
		Order("w.created_at").Scan(&out).Error
	return out, err
}

// offerFreedSlot ofrece una hora que se liberó al siguiente de la lista de ese día. Se llama al cancelar una cita y
// cuando alguien no respondió a tiempo. Si la hora ya está muy cerca o vuelve a estar ocupada, no se ofrece.
func (s *AgendaService) offerFreedSlot(entityID uuid.UUID, slot time.Time) {
	now := time.Now()
	if slot.Sub(now) < minLeadForOffer {
		return
	}
	// ¿Sigue libre? Una cita o un bloqueo nuevos la pudieron tomar.
	busy, err := s.busy(entityID, slot, slot.Add(time.Minute))
	if err != nil || len(busy) > 0 {
		return
	}
	var entries []models.WaitlistEntry
	if err := s.db.Where("entity_id = ? AND day = ? AND status = ?", entityID, DayKey(slot), models.WaitWaiting).Find(&entries).Error; err != nil {
		log.Printf("lista de espera: no se pudo leer la fila: %v", err)
		return
	}
	next := NextWaiting(entries)
	if next == nil {
		return
	}
	res := s.db.Model(&models.WaitlistEntry{}).Where("id = ? AND status = ?", next.ID, models.WaitWaiting).
		Updates(map[string]interface{}{"status": models.WaitOffered, "offered_slot": slot, "offered_at": now})
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	e, err := s.entity(entityID)
	if err != nil {
		return
	}
	s.notify(next.UserID, "Se liberó una hora en "+e.Name,
		"Las "+formatWhen(slot)+". Tienes 10 minutos para tomarla.",
		map[string]string{"type": "waitlist_offer", "waitlist_id": next.ID.String(), "entity_id": entityID.String()}, "", true)
}

// TakeWaitlistOffer: la persona toma la hora que se le ofreció, dentro de su tiempo. Queda como su cita.
func (s *AgendaService) TakeWaitlistOffer(userID, id uuid.UUID) (*models.Appointment, error) {
	var w models.WaitlistEntry
	if err := s.db.First(&w, "id = ? AND user_id = ?", id, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApptNotFound
		}
		return nil, err
	}
	if w.Status != models.WaitOffered || w.OfferedSlot == nil || OfferExpired(w.OfferedAt, time.Now()) {
		return nil, ErrOfferGone
	}
	a, err := s.createAppointment(userID, w.EntityID, w.ServiceID, *w.OfferedSlot, "Tomó una hora de la lista de espera", "waitlist:"+w.ID.String())
	if err != nil {
		return nil, err
	}
	s.db.Model(&models.WaitlistEntry{}).Where("id = ?", w.ID).Updates(map[string]interface{}{"status": models.WaitBooked, "appointment_id": a.ID})
	return a, nil
}

// passExpiredOffers: a quien no respondió a tiempo se le pasa la hora al siguiente de la fila.
func (s *AgendaService) passExpiredOffers(now time.Time) {
	var due []models.WaitlistEntry
	if err := s.db.Where("status = ? AND offered_at <= ?", models.WaitOffered, now.Add(-OfferWindow)).Find(&due).Error; err != nil {
		return
	}
	for _, w := range due {
		res := s.db.Model(&models.WaitlistEntry{}).Where("id = ? AND status = ?", w.ID, models.WaitOffered).Update("status", models.WaitPassed)
		if res.Error == nil && res.RowsAffected > 0 && w.OfferedSlot != nil {
			s.offerFreedSlot(w.EntityID, *w.OfferedSlot)
		}
	}
}

// ColombiaLocation es el huso de la agenda, para leer los días que manda la app.
func ColombiaLocation() *time.Location { return nowCO().Location() }
