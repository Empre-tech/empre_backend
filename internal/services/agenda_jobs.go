package services

import (
	"context"
	"log"
	"strconv"
	"time"

	"empre_backend/internal/agenda"
	"empre_backend/internal/models"
)

// StartAgendaJobs runs the single background loop of the agenda: it expires
// unanswered requests, sends reminders and closes finished appointments. It
// stops when ctx is cancelled. Every step is idempotent (guarded by status
// and "already sent" marks), so a restart or a second instance is harmless.
func (s *AgendaService) StartAgendaJobs(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(agendaTickEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.safeTick()
			}
		}
	}()
}

func (s *AgendaService) safeTick() {
	defer func() {
		if r := recover(); r != nil {
			log.Println("agenda: tick panic:", r)
		}
	}()
	s.Tick(time.Now())
}

// Tick runs one pass of every job at instant now.
func (s *AgendaService) Tick(now time.Time) {
	s.expireRequests(now)
	s.remindHalfway(now)
	s.remindUpcoming(now)
	s.promptClosing(now)
	s.closeOverdue(now)
}

func (s *AgendaService) expireRequests(now time.Time) {
	var due []models.Appointment
	if err := s.db.Select("id", "entity_id", "customer_id", "starts_at").
		Where("kind = ? AND status = ? AND expires_at <= ?", models.AppointmentBooking, models.ApptRequested, now).
		Limit(200).Find(&due).Error; err != nil {
		log.Println("agenda: expire query:", err)
		return
	}
	for i := range due {
		a := &due[i]
		res := s.db.Model(&models.Appointment{}).
			Where("id = ? AND status = ?", a.ID, models.ApptRequested).
			Update("status", models.ApptExpired)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		e, err := s.entity(a.EntityID)
		if err != nil || a.CustomerID == nil {
			continue
		}
		s.notify(*a.CustomerID, e.Name+" no alcanzó a responder",
			"Tu solicitud para "+formatWhen(a.StartsAt)+" venció. Puedes pedir otra hora.", apptData("appointment_expired", a), "", false)
		s.broadcast(a.ID, *a.CustomerID, e.OwnerID)
	}
}

// remindHalfway nudges the business when half of the answer window passed
// (capped at 12h for long windows), unless it is quiet hours.
func (s *AgendaService) remindHalfway(now time.Time) {
	if agenda.InSilence(now.In(nowCO().Location())) {
		return
	}
	var rows []models.Appointment
	if err := s.db.Where("kind = ? AND status = ? AND halfway_reminded_at IS NULL AND expires_at > ?",
		models.AppointmentBooking, models.ApptRequested, now.Add(agenda.MinWindow)).
		Limit(200).Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		a := &rows[i]
		if a.ExpiresAt == nil || agenda.HalfwayPoint(a.CreatedAt, *a.ExpiresAt).After(now) {
			continue
		}
		res := s.db.Model(&models.Appointment{}).
			Where("id = ? AND halfway_reminded_at IS NULL", a.ID).Update("halfway_reminded_at", now)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		e, err := s.entity(a.EntityID)
		if err != nil {
			continue
		}
		left := a.ExpiresAt.Sub(now).Round(time.Minute)
		s.notify(e.OwnerID, "Solicitud por expirar", "Quedan "+humanDuration(left)+" para responder la cita de "+formatWhen(a.StartsAt),
			apptData("appointment_request", a), "appointment_request", true)
	}
}

// remindUpcoming sends the 24h and 2h reminders for confirmed appointments.
func (s *AgendaService) remindUpcoming(now time.Time) {
	var rows []models.Appointment
	if err := s.db.Where("kind = ? AND status = ? AND starts_at > ? AND starts_at <= ? AND (reminded24h_at IS NULL OR reminded2h_at IS NULL)",
		models.AppointmentBooking, models.ApptConfirmed, now, now.Add(24*time.Hour)).
		Limit(200).Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		a := &rows[i]
		until := a.StartsAt.Sub(now)
		col, label := "", ""
		switch {
		case until <= 2*time.Hour && a.Reminded2hAt == nil:
			col, label = "reminded2h_at", "en 2 horas"
		case until > 2*time.Hour && a.Reminded24hAt == nil:
			col, label = "reminded24h_at", "mañana"
		default:
			continue
		}
		res := s.db.Model(&models.Appointment{}).Where("id = ? AND "+col+" IS NULL", a.ID).Update(col, now)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		e, err := s.entity(a.EntityID)
		if err != nil {
			continue
		}
		body := "Tu cita con " + e.Name + " es " + label + " (" + formatWhen(a.StartsAt) + ")"
		if a.CustomerID != nil {
			s.notify(*a.CustomerID, "Recordatorio de cita", body, apptData("appointment_reminder", a), "", true)
		}
		s.notify(e.OwnerID, "Recordatorio de cita", "Tienes una cita "+label+": "+formatWhen(a.StartsAt), apptData("appointment_reminder", a), "", true)
	}
}

// promptClosing asks each side that has not answered "¿se dio la cita?", 30
// minutes and again 24 hours after the appointment ended.
func (s *AgendaService) promptClosing(now time.Time) {
	var rows []models.Appointment
	if err := s.db.Where("kind = ? AND status = ? AND ends_at <= ? AND (closing_prompted_at IS NULL OR closing_reminded24_at IS NULL)",
		models.AppointmentBooking, models.ApptConfirmed, now.Add(-30*time.Minute)).
		Limit(200).Find(&rows).Error; err != nil {
		return
	}
	for i := range rows {
		a := &rows[i]
		col := ""
		switch {
		case a.ClosingPromptedAt == nil:
			col = "closing_prompted_at"
		case a.ClosingReminded24At == nil && !now.Before(a.EndsAt.Add(24*time.Hour)):
			col = "closing_reminded24_at"
		default:
			continue
		}
		res := s.db.Model(&models.Appointment{}).Where("id = ? AND "+col+" IS NULL", a.ID).Update(col, now)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		e, err := s.entity(a.EntityID)
		if err != nil {
			continue
		}
		data := apptData("appointment_closing", a)
		if a.CustomerAttended == nil && a.CustomerID != nil {
			s.notify(*a.CustomerID, "¿Se dio la cita?", "Cuéntanos cómo te fue con "+e.Name, data, "", true)
		}
		if a.BusinessAttended == nil {
			s.notify(e.OwnerID, "¿Se dio la cita?", "Confirma si el cliente llegó a su cita de "+formatWhen(a.StartsAt), data, "", true)
		}
	}
}

// closeOverdue resolves appointments whose closing deadline passed with
// whatever answers exist (none = completed).
func (s *AgendaService) closeOverdue(now time.Time) {
	var ids []models.Appointment
	if err := s.db.Select("id", "entity_id", "customer_id").
		Where("kind = ? AND status = ? AND closing_deadline IS NOT NULL AND closing_deadline <= ?",
			models.AppointmentBooking, models.ApptConfirmed, now).
		Limit(200).Find(&ids).Error; err != nil {
		return
	}
	for i := range ids {
		s.resolveOutcome(ids[i].ID, true)
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "menos de un minuto"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d/time.Minute)) + " minutos"
	}
	h := int(d / time.Hour)
	if h == 1 {
		return "1 hora"
	}
	return strconv.Itoa(h) + " horas"
}
