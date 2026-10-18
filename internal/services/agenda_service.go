package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"empre_backend/internal/agenda"
	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Errors the handlers translate into HTTP responses.
var (
	ErrAgendaDisabled = errors.New("agenda_disabled")
	ErrEntityHidden   = errors.New("entity_hidden")
	ErrApptNotFound   = errors.New("appointment_not_found")
	ErrApptForbidden  = errors.New("forbidden")
	ErrBadState       = errors.New("bad_state")
	ErrTooManyPending = errors.New("too_many_pending")
	ErrCustomerBusy   = errors.New("customer_busy")
	ErrOwnBusiness    = errors.New("own_business")
)

// InvalidError is a validation problem with a message for the client.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// SlotTakenError means the requested start is not available (taken, outside
// hours, too soon...). Alternatives are the closest free starts.
type SlotTakenError struct{ Alternatives []time.Time }

func (e *SlotTakenError) Error() string { return "slot_taken" }

// OverlapsBookingError means a manual block collides with live appointments.
type OverlapsBookingError struct{ Appointments []models.Appointment }

func (e *OverlapsBookingError) Error() string { return "overlaps_booking" }

// Realtime lets the service push frames to connected users (implemented by
// the WebSocket hub; an interface avoids an import cycle).
type Realtime interface {
	SendFrame(userID uuid.UUID, data []byte) bool
	IsOnline(userID uuid.UUID) bool
}

const (
	maxPendingPerCustomerBusiness = 3
	lateCancelWindow              = 2 * time.Hour
	closingDeadlineAfterEnd       = 48 * time.Hour
	agendaTickEvery               = 30 * time.Second
)

var declineReasons = map[string]string{
	"no_space":   "Ya no tengo ese espacio",
	"no_service": "No presto ese servicio",
	"other":      "Otro motivo",
}

type AgendaService struct {
	db   *gorm.DB
	push *PushService
	rt   Realtime
}

func NewAgendaService(db *gorm.DB, push *PushService) *AgendaService {
	return &AgendaService{db: db, push: push}
}

// SetRealtime wires the hub once it exists.
func (s *AgendaService) SetRealtime(rt Realtime) { s.rt = rt }

func nowCO() time.Time { return utils.NowInCartagena() }

// ---------- configuration ----------

func defaultSettings(entityID uuid.UUID) models.AgendaSettings {
	return models.AgendaSettings{EntityID: entityID, SlotMinutes: 30, MinNoticeMin: agenda.MinNoticeFloor, HorizonDays: 14}
}

func (s *AgendaService) settings(entityID uuid.UUID) (models.AgendaSettings, error) {
	var st models.AgendaSettings
	err := s.db.Where("entity_id = ?", entityID).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaultSettings(entityID), nil
	}
	return st, err
}

func (s *AgendaService) hours(entityID uuid.UUID) ([]agenda.Hours, error) {
	var rows []models.BusinessHour
	if err := s.db.Where("entity_id = ?", entityID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]agenda.Hours, 0, len(rows))
	for _, r := range rows {
		out = append(out, agenda.Hours{Weekday: r.Weekday, Closed: r.Closed, Is24h: r.Is24h, Open: r.OpenTime, Close: r.CloseTime})
	}
	return out, nil
}

type entityRef struct {
	ID      uuid.UUID
	OwnerID uuid.UUID
	Name    string
}

func (s *AgendaService) entity(entityID uuid.UUID) (entityRef, error) {
	var e models.Entity
	if err := s.db.Select("id", "owner_id", "name").First(&e, "id = ?", entityID).Error; err != nil {
		return entityRef{}, err
	}
	return entityRef{ID: e.ID, OwnerID: e.OwnerID, Name: e.Name}, nil
}

func (s *AgendaService) requireOwner(ownerID, entityID uuid.UUID) (entityRef, error) {
	e, err := s.entity(entityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return e, ErrApptNotFound
		}
		return e, err
	}
	if e.OwnerID != ownerID {
		return e, ErrApptForbidden
	}
	return e, nil
}

// GetSettings returns the settings (defaults, disabled, if never saved).
func (s *AgendaService) GetSettings(ownerID, entityID uuid.UUID) (models.AgendaSettings, error) {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return models.AgendaSettings{}, err
	}
	return s.settings(entityID)
}

// SettingsInput is what the business can change.
type SettingsInput struct {
	Enabled      bool   `json:"enabled"`
	SlotMinutes  int    `json:"slot_minutes"`
	LunchStart   string `json:"lunch_start"`
	LunchEnd     string `json:"lunch_end"`
	BufferMin    int    `json:"buffer_min"`
	MinNoticeMin int    `json:"min_notice_min"`
	HorizonDays  int    `json:"horizon_days"`
	// Recepción y avisos (§9b). Opcionales: nil deja lo guardado, así una app vieja no los borra.
	IntakeMode       *string `json:"intake_mode"`
	ReviewNoShows    *int    `json:"review_no_shows"`
	ReviewFirstVisit *bool   `json:"review_first_visit"`
	ReviewNoticeMin  *int    `json:"review_notice_min"`
	NotifyEvery      *int    `json:"notify_every"`
	QuietMode        *string `json:"quiet_mode"`
	QuietFrom        *string `json:"quiet_from"`
	QuietTo          *string `json:"quiet_to"`
}

// applyIntake valida y pone en st la recepción y los avisos que llegaron.
func applyIntake(st *models.AgendaSettings, in SettingsInput) error {
	if in.IntakeMode != nil {
		switch *in.IntakeMode {
		case agenda.IntakeManual, agenda.IntakeRules, agenda.IntakeAuto:
			st.IntakeMode = *in.IntakeMode
		default:
			return &InvalidError{"intake_mode debe ser manual, rules o auto"}
		}
	}
	if in.ReviewNoShows != nil {
		if *in.ReviewNoShows < 0 || *in.ReviewNoShows > 10 {
			return &InvalidError{"review_no_shows debe estar entre 0 y 10"}
		}
		st.ReviewNoShows = *in.ReviewNoShows
	}
	if in.ReviewFirstVisit != nil {
		st.ReviewFirstVisit = *in.ReviewFirstVisit
	}
	if in.ReviewNoticeMin != nil {
		if *in.ReviewNoticeMin < 0 || *in.ReviewNoticeMin > 1440 {
			return &InvalidError{"review_notice_min debe estar entre 0 y 1440"}
		}
		st.ReviewNoticeMin = *in.ReviewNoticeMin
	}
	if in.NotifyEvery != nil {
		switch *in.NotifyEvery {
		case 0, 30, 60, 360:
			st.NotifyEvery = *in.NotifyEvery
		default:
			return &InvalidError{"notify_every debe ser 0, 30, 60 o 360"}
		}
	}
	if in.QuietFrom != nil {
		st.QuietFrom = *in.QuietFrom
	}
	if in.QuietTo != nil {
		st.QuietTo = *in.QuietTo
	}
	if in.QuietMode != nil {
		switch *in.QuietMode {
		case agenda.QuietHours, agenda.QuietOff:
		case agenda.QuietCustom:
			if !agenda.ValidClock(st.QuietFrom) || !agenda.ValidClock(st.QuietTo) || st.QuietFrom == st.QuietTo {
				return &InvalidError{"el silencio necesita desde y hasta distintos (HH:MM)"}
			}
		default:
			return &InvalidError{"quiet_mode debe ser hours, custom u off"}
		}
		st.QuietMode = *in.QuietMode
	}
	return nil
}

// quietFor dice si en t no se le avisa al dueño de ese negocio.
func (s *AgendaService) quietFor(st models.AgendaSettings, entityID uuid.UUID, t time.Time) bool {
	hrs, err := s.hours(entityID)
	if err != nil {
		return false
	}
	return agenda.InQuiet(agenda.Quiet{Mode: st.QuietMode, From: st.QuietFrom, To: st.QuietTo}, hrs, t.In(nowCO().Location()))
}

func validClock(v string) bool {
	if v == "" {
		return true
	}
	_, _, ok := parseHHMM(v)
	return ok
}

func parseHHMM(v string) (int, int, bool) {
	var h, m int
	if n, err := fmt.Sscanf(v, "%d:%d", &h, &m); err != nil || n != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

func (s *AgendaService) UpdateSettings(ownerID, entityID uuid.UUID, in SettingsInput) (models.AgendaSettings, error) {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return models.AgendaSettings{}, err
	}
	switch in.SlotMinutes {
	case 15, 30, 45, 60:
	default:
		return models.AgendaSettings{}, &InvalidError{"slot_minutes debe ser 15, 30, 45 o 60"}
	}
	if !validClock(in.LunchStart) || !validClock(in.LunchEnd) || (in.LunchStart == "") != (in.LunchEnd == "") {
		return models.AgendaSettings{}, &InvalidError{"el almuerzo necesita hora de inicio y de fin válidas (HH:MM)"}
	}
	if in.LunchStart != "" && in.LunchEnd <= in.LunchStart {
		return models.AgendaSettings{}, &InvalidError{"el almuerzo debe terminar después de empezar"}
	}
	if in.BufferMin < 0 || in.BufferMin > 60 {
		return models.AgendaSettings{}, &InvalidError{"buffer_min debe estar entre 0 y 60"}
	}
	if in.MinNoticeMin < agenda.MinNoticeFloor {
		in.MinNoticeMin = agenda.MinNoticeFloor
	}
	if in.HorizonDays < 1 || in.HorizonDays > 60 {
		return models.AgendaSettings{}, &InvalidError{"horizon_days debe estar entre 1 y 60"}
	}

	st, err := s.settings(entityID)
	if err != nil {
		return st, err
	}
	st.Enabled = in.Enabled
	st.SlotMinutes = in.SlotMinutes
	st.LunchStart = in.LunchStart
	st.LunchEnd = in.LunchEnd
	st.BufferMin = in.BufferMin
	st.MinNoticeMin = in.MinNoticeMin
	st.HorizonDays = in.HorizonDays
	if err := applyIntake(&st, in); err != nil {
		return st, err
	}
	if err := s.db.Save(&st).Error; err != nil {
		return st, err
	}
	return st, nil
}

// ---------- services ----------

// BookableService es un servicio del catálogo visto desde la agenda. Conserva el contrato que la app ya usa
// ({id, entity_id, name, duration_min, active}); `active` = se puede reservar.
type BookableService struct {
	ID          uuid.UUID `json:"id"`
	EntityID    uuid.UUID `json:"entity_id"`
	Name        string    `json:"name"`
	DurationMin int       `json:"duration_min"`
	Active      bool      `json:"active"`
}

func asBookable(it models.ServiceItem) BookableService {
	d := 0
	if it.DurationMin != nil {
		d = *it.DurationMin
	}
	return BookableService{ID: it.ID, EntityID: it.EntityID, Name: it.Name, DurationMin: d, Active: it.Bookable}
}

// ListServices devuelve los servicios del catálogo con duración (onlyActive: solo los reservables).
func (s *AgendaService) ListServices(entityID uuid.UUID, onlyActive bool) ([]BookableService, error) {
	var items []models.ServiceItem
	q := s.db.Where("entity_id = ? AND status = ? AND duration_min IS NOT NULL", entityID, models.ServiceActive)
	if onlyActive {
		q = q.Where("bookable = ?", true)
	}
	if err := q.Order("position ASC, created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]BookableService, 0, len(items))
	for _, it := range items {
		out = append(out, asBookable(it))
	}
	return out, nil
}

type AgendaServiceInput struct {
	Name        string `json:"name"`
	DurationMin int    `json:"duration_min"`
	Active      *bool  `json:"active"`
}

func (in AgendaServiceInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return &InvalidError{"el servicio necesita nombre"}
	}
	if in.DurationMin < 5 || in.DurationMin > 480 {
		return &InvalidError{"duration_min debe estar entre 5 y 480"}
	}
	return nil
}

// CreateService crea el servicio en el catálogo, ya reservable (mismo tope de servicios activos que el catálogo).
func (s *AgendaService) CreateService(ownerID, entityID uuid.UUID, in AgendaServiceInput) (BookableService, error) {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return BookableService{}, err
	}
	if err := in.validate(); err != nil {
		return BookableService{}, err
	}
	var active int64
	if err := s.db.Model(&models.ServiceItem{}).Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Count(&active).Error; err != nil {
		return BookableService{}, err
	}
	if active >= MaxActiveServices {
		return BookableService{}, &InvalidError{"llegaste al tope de servicios del catálogo"}
	}
	d := in.DurationMin
	it := models.ServiceItem{EntityID: entityID, Name: strings.TrimSpace(in.Name), DurationMin: &d, Bookable: true, Status: models.ServiceActive, Position: int(active)}
	if err := s.db.Create(&it).Error; err != nil {
		return BookableService{}, err
	}
	return asBookable(it), nil
}

func (s *AgendaService) UpdateService(ownerID, entityID, serviceID uuid.UUID, in AgendaServiceInput) (BookableService, error) {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return BookableService{}, err
	}
	if err := in.validate(); err != nil {
		return BookableService{}, err
	}
	var it models.ServiceItem
	if err := s.db.First(&it, "id = ? AND entity_id = ? AND status = ?", serviceID, entityID, models.ServiceActive).Error; err != nil {
		return BookableService{}, ErrApptNotFound
	}
	it.Name = strings.TrimSpace(in.Name)
	d := in.DurationMin
	it.DurationMin = &d
	if in.Active != nil {
		it.Bookable = *in.Active
	}
	if err := s.db.Model(&it).Select("name", "duration_min", "bookable").Updates(&it).Error; err != nil {
		return BookableService{}, err
	}
	return asBookable(it), nil
}

// DeleteService deja de ofrecer el servicio para reservar; sigue en el catálogo y las citas pasadas lo conservan.
func (s *AgendaService) DeleteService(ownerID, entityID, serviceID uuid.UUID) error {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return err
	}
	res := s.db.Model(&models.ServiceItem{}).Where("id = ? AND entity_id = ?", serviceID, entityID).Update("bookable", false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrApptNotFound
	}
	return nil
}

// durationFor returns the minutes an appointment occupies: the service's own
// duration, or the grid size when the business has no services.
func (s *AgendaService) durationFor(entityID uuid.UUID, serviceID *uuid.UUID, st models.AgendaSettings) (int, error) {
	if serviceID == nil {
		active, err := s.ListServices(entityID, true)
		if err != nil {
			return 0, err
		}
		if len(active) > 0 {
			return 0, &InvalidError{"elige un servicio"}
		}
		return st.SlotMinutes, nil
	}
	var it models.ServiceItem
	if err := s.db.First(&it, "id = ? AND entity_id = ? AND status = ? AND bookable = ? AND duration_min IS NOT NULL", *serviceID, entityID, models.ServiceActive, true).Error; err != nil {
		return 0, &InvalidError{"servicio no válido"}
	}
	return *it.DurationMin, nil
}

// ---------- availability ----------

// entityHidden mirrors the discovery rule: a business over the free-trial
// threshold, unpaid and past the grace period is hidden from NEW customers.
func (s *AgendaService) entityHidden(entityID uuid.UUID) bool {
	var n int64
	s.db.Raw(`SELECT COUNT(*) FROM subscriptions sub
		WHERE sub.entity_id = ? AND sub.status != 'active'
		AND sub.trial_threshold_notified_at IS NOT NULL
		AND sub.trial_threshold_notified_at < NOW() - INTERVAL '7 days'`, entityID).Scan(&n)
	return n > 0
}

func (s *AgendaService) hasHistory(userID, entityID uuid.UUID) bool {
	var n int64
	s.db.Model(&models.Message{}).Where("entity_id = ? AND user_id = ?", entityID, userID).Limit(1).Count(&n)
	if n > 0 {
		return true
	}
	s.db.Model(&models.Appointment{}).Where("entity_id = ? AND customer_id = ?", entityID, userID).Limit(1).Count(&n)
	return n > 0
}

// canSee: the owner always; anyone else unless the business is hidden and
// they have no prior conversation or appointment with it.
func (s *AgendaService) canSee(userID uuid.UUID, e entityRef) bool {
	if e.OwnerID == userID {
		return true
	}
	return !s.entityHidden(e.ID) || s.hasHistory(userID, e.ID)
}

func (s *AgendaService) busy(entityID uuid.UUID, from, to time.Time) ([]agenda.Interval, error) {
	var rows []models.Appointment
	err := s.db.Select("starts_at", "blocks_until").
		Where("entity_id = ? AND status IN ? AND starts_at < ? AND blocks_until > ?",
			entityID, []models.AppointmentStatus{models.ApptRequested, models.ApptConfirmed, models.ApptBlocked}, to, from).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]agenda.Interval, 0, len(rows))
	for _, r := range rows {
		out = append(out, agenda.Interval{Start: r.StartsAt.In(nowCO().Location()), End: r.BlocksUntil.In(nowCO().Location())})
	}
	return out, nil
}

func (s *AgendaService) slotsForDay(st models.AgendaSettings, hrs []agenda.Hours, entityID uuid.UUID, day time.Time, durationMin int) ([]time.Time, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	busy, err := s.busy(entityID, dayStart.AddDate(0, 0, -1), dayStart.AddDate(0, 0, 2))
	if err != nil {
		return nil, err
	}
	return agenda.FreeSlots(agenda.SlotParams{
		Day: dayStart, Now: nowCO(), Hours: hrs, SlotMinutes: st.SlotMinutes, DurationMin: durationMin,
		BufferMin: st.BufferMin, LunchStart: st.LunchStart, LunchEnd: st.LunchEnd,
		MinNoticeMin: st.MinNoticeMin, HorizonDays: st.HorizonDays, Busy: busy,
	}), nil
}

// Availability returns the free start times of one day for the viewer.
func (s *AgendaService) Availability(viewerID, entityID uuid.UUID, day time.Time, serviceID *uuid.UUID) ([]time.Time, error) {
	e, err := s.entity(entityID)
	if err != nil {
		return nil, ErrApptNotFound
	}
	st, err := s.settings(entityID)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, ErrAgendaDisabled
	}
	if !s.canSee(viewerID, e) {
		return nil, ErrEntityHidden
	}
	dur, err := s.durationFor(entityID, serviceID, st)
	if err != nil {
		return nil, err
	}
	hrs, err := s.hours(entityID)
	if err != nil {
		return nil, err
	}
	return s.slotsForDay(st, hrs, entityID, day.In(nowCO().Location()), dur)
}

// Summary is the profile's availability signal.
type Summary struct {
	Enabled     bool              `json:"enabled"`
	NextSlot    *time.Time        `json:"next_slot,omitempty"`
	SlotMinutes int               `json:"slot_minutes"`
	HorizonDays int               `json:"horizon_days"`
	Services    []BookableService `json:"services"`
}

func (s *AgendaService) Summary(viewerID, entityID uuid.UUID) (Summary, error) {
	out := Summary{Services: []BookableService{}}
	e, err := s.entity(entityID)
	if err != nil {
		return out, ErrApptNotFound
	}
	st, err := s.settings(entityID)
	if err != nil {
		return out, err
	}
	if !st.Enabled || !s.canSee(viewerID, e) {
		return out, nil
	}
	out.Enabled = true
	out.SlotMinutes = st.SlotMinutes
	out.HorizonDays = st.HorizonDays
	if out.Services, err = s.ListServices(entityID, true); err != nil {
		return out, err
	}
	dur := st.SlotMinutes
	if len(out.Services) > 0 {
		dur = out.Services[0].DurationMin
		for _, v := range out.Services {
			if v.DurationMin < dur {
				dur = v.DurationMin
			}
		}
	}
	hrs, err := s.hours(entityID)
	if err != nil {
		return out, err
	}
	now := nowCO()
	busy, err := s.busy(entityID, now, now.AddDate(0, 0, st.HorizonDays+1))
	if err != nil {
		return out, err
	}
	for i := 0; i <= st.HorizonDays; i++ {
		day := time.Date(now.Year(), now.Month(), now.Day()+i, 0, 0, 0, 0, now.Location())
		slots := agenda.FreeSlots(agenda.SlotParams{
			Day: day, Now: now, Hours: hrs, SlotMinutes: st.SlotMinutes, DurationMin: dur,
			BufferMin: st.BufferMin, LunchStart: st.LunchStart, LunchEnd: st.LunchEnd,
			MinNoticeMin: st.MinNoticeMin, HorizonDays: st.HorizonDays, Busy: busy,
		})
		if len(slots) > 0 {
			t := slots[0]
			out.NextSlot = &t
			break
		}
	}
	return out, nil
}

// ---------- formatting / notifications ----------

var weekdaysES = []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
var monthsES = []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

// formatWhen renders a time the way Colombians say it: "hoy 3:00 p.m.".
func formatWhen(t time.Time) string {
	t = t.In(nowCO().Location())
	now := nowCO()
	h := t.Hour() % 12
	if h == 0 {
		h = 12
	}
	suffix := "a.m."
	if t.Hour() >= 12 {
		suffix = "p.m."
	}
	clock := fmt.Sprintf("%d:%02d %s", h, t.Minute(), suffix)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	switch int(day.Sub(today).Hours() / 24) {
	case 0:
		return "hoy " + clock
	case 1:
		return "mañana " + clock
	}
	return fmt.Sprintf("%s %d %s %s", weekdaysES[t.Weekday()], t.Day(), monthsES[t.Month()-1], clock)
}

type apptFrame struct {
	Event       string              `json:"event"`
	Appointment *models.Appointment `json:"appointment"`
}

func (s *AgendaService) load(id uuid.UUID) (*models.Appointment, error) {
	var a models.Appointment
	err := s.db.Preload("Entity").Preload("Customer").Preload("Service").First(&a, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrApptNotFound
		}
		return nil, err
	}
	hydrate(&a)
	return &a, nil
}

func hydrate(a *models.Appointment) {
	if a.ProposedSlots != "" {
		var raw []time.Time
		if err := json.Unmarshal([]byte(a.ProposedSlots), &raw); err == nil {
			a.Proposed = raw
		}
	}
}

// broadcast tells every given connected user that the appointment changed.
func (s *AgendaService) broadcast(id uuid.UUID, users ...uuid.UUID) {
	if s.rt == nil {
		return
	}
	a, err := s.load(id)
	if err != nil {
		return
	}
	data, err := json.Marshal(apptFrame{Event: "appointment_updated", Appointment: a})
	if err != nil {
		return
	}
	seen := map[uuid.UUID]bool{}
	for _, u := range users {
		if u == uuid.Nil || seen[u] {
			continue
		}
		seen[u] = true
		s.rt.SendFrame(u, data)
	}
}

// notify sends a push. alwaysPush forces it even when the user is online
// (time-sensitive reminders); otherwise it is skipped for connected users,
// who already got the WebSocket frame.
func (s *AgendaService) notify(userID uuid.UUID, title, body string, data map[string]string, category string, alwaysPush bool) {
	if s.push == nil || userID == uuid.Nil {
		return
	}
	if !alwaysPush && s.rt != nil && s.rt.IsOnline(userID) {
		return
	}
	s.push.NotifyWithCategory(userID, title, body, data, category)
}

func apptData(kind string, a *models.Appointment) map[string]string {
	return map[string]string{"type": kind, "appointment_id": a.ID.String(), "entity_id": a.EntityID.String()}
}

// ---------- booking ----------

func isOverlapViolation(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "appointments_no_overlap") || strings.Contains(m, "23P01")
}

// isRetryableConflict: interbloqueo o serialización. Con el candado por negocio no debería pasar; si pasa, se trata
// como «hora ocupada» (con alternativas) y nunca como error del servidor.
func isRetryableConflict(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "40P01") || strings.Contains(m, "40001") || strings.Contains(m, "deadlock detected")
}

func isIdemViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "appointments_idem")
}

// CreateAppointment is a customer asking for a space. `idemKey` (opcional) es la clave del intento que manda la app:
// un reintento con la misma clave devuelve la misma cita sin crear otra, incluso si llega a la vez que el original
// (la copia tardía vería «hora ocupada» por su propio gemelo; aquí se devuelve la cita que sí se creó).
func (s *AgendaService) CreateAppointment(customerID, entityID uuid.UUID, serviceID *uuid.UUID, startsAt time.Time, note, idemKey string) (*models.Appointment, error) {
	if prev := s.byIdemKey(customerID, idemKey); prev != nil {
		return prev, nil
	}
	a, err := s.createAppointment(customerID, entityID, serviceID, startsAt, note, idemKey)
	if err != nil {
		if prev := s.byIdemKey(customerID, idemKey); prev != nil {
			return prev, nil
		}
	}
	return a, err
}

func (s *AgendaService) byIdemKey(customerID uuid.UUID, idemKey string) *models.Appointment {
	if idemKey == "" {
		return nil
	}
	var prev models.Appointment
	if s.db.Select("id").Where("customer_id = ? AND idempotency_key = ?", customerID, idemKey).Take(&prev).Error != nil {
		return nil
	}
	a, err := s.load(prev.ID)
	if err != nil {
		return nil
	}
	return a
}

func (s *AgendaService) createAppointment(customerID, entityID uuid.UUID, serviceID *uuid.UUID, startsAt time.Time, note, idemKey string) (*models.Appointment, error) {
	e, err := s.entity(entityID)
	if err != nil {
		return nil, ErrApptNotFound
	}
	if e.OwnerID == customerID {
		return nil, ErrOwnBusiness
	}
	st, err := s.settings(entityID)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, ErrAgendaDisabled
	}
	if !s.canSee(customerID, e) {
		return nil, ErrEntityHidden
	}
	dur, err := s.durationFor(entityID, serviceID, st)
	if err != nil {
		return nil, err
	}
	hrs, err := s.hours(entityID)
	if err != nil {
		return nil, err
	}

	start := startsAt.In(nowCO().Location()).Truncate(time.Minute)
	slots, err := s.slotsForDay(st, hrs, entityID, start, dur)
	if err != nil {
		return nil, err
	}
	free := false
	for _, sl := range slots {
		if sl.Equal(start) {
			free = true
			break
		}
	}
	if !free {
		return nil, &SlotTakenError{Alternatives: s.alternatives(st, hrs, entityID, start, dur, slots)}
	}

	var pending int64
	s.db.Model(&models.Appointment{}).Where("entity_id = ? AND customer_id = ? AND status = ?", entityID, customerID, models.ApptRequested).Count(&pending)
	if pending >= maxPendingPerCustomerBusiness {
		return nil, ErrTooManyPending
	}
	end := start.Add(time.Duration(dur) * time.Minute)
	var clash int64
	s.db.Model(&models.Appointment{}).
		Where("customer_id = ? AND status IN ? AND starts_at < ? AND ends_at > ?", customerID,
			[]models.AppointmentStatus{models.ApptRequested, models.ApptConfirmed}, end, start).Count(&clash)
	if clash > 0 {
		return nil, ErrCustomerBusy
	}

	// ¿Se confirma sola o espera al dueño? (modo y reglas del negocio, con el historial de este cliente aquí)
	var hist agenda.History
	var completed, noShows int64
	s.db.Model(&models.Appointment{}).Where("entity_id = ? AND customer_id = ? AND status = ?", entityID, customerID, models.ApptCompleted).Count(&completed)
	s.db.Model(&models.Appointment{}).Where("entity_id = ? AND customer_id = ? AND status = ?", entityID, customerID, models.ApptNoShow).Count(&noShows)
	hist.Completed, hist.NoShows = int(completed), int(noShows)
	now := nowCO()
	review, _ := agenda.NeedsReview(agenda.Intake{Mode: st.IntakeMode, ReviewNoShows: st.ReviewNoShows, ReviewFirstVisit: st.ReviewFirstVisit, ReviewNoticeMin: st.ReviewNoticeMin}, hist, start, now)

	appt := models.Appointment{
		EntityID: entityID, Kind: models.AppointmentBooking, CustomerID: &customerID, ServiceID: serviceID,
		StartsAt: start, EndsAt: end, BlocksUntil: end.Add(time.Duration(st.BufferMin) * time.Minute),
		Status: models.ApptRequested, CustomerNote: strings.TrimSpace(note),
	}
	cardText := "Pidió una cita para " + formatWhen(start)
	if review {
		expires := agenda.ComputeExpiry(now, start, hrs)
		appt.ExpiresAt = &expires
	} else {
		deadline := end.Add(closingDeadlineAfterEnd)
		respondedAt := time.Now()
		appt.Status, appt.RespondedAt, appt.ClosingDeadline = models.ApptConfirmed, &respondedAt, &deadline
		cardText = "Reservó una cita para " + formatWhen(start)
	}
	if idemKey != "" {
		k := idemKey
		appt.IdempotencyKey = &k
	}
	// El dueño recibe el aviso al instante solo si lo pidió así y no está en silencio; si no, va en el resumen.
	instant := st.NotifyEvery == 0 && !s.quietFor(st, entityID, time.Now())
	if instant {
		t := time.Now()
		appt.OwnerNotifiedAt = &t
	}
	var card models.Message
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// Las reservas de un mismo negocio pasan en fila: con muchas a la vez, la restricción EXCLUDE sola produce
		// interbloqueos (probado: 1 de cada 3 corridas con 100 clientes). Negocios distintos no se esperan.
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?::text, 0))", entityID.String()).Error; err != nil {
			return err
		}
		if err := tx.Omit(clause.Associations).Create(&appt).Error; err != nil {
			return err
		}
		card = models.Message{
			SenderID: customerID, EntityID: entityID, UserID: customerID, SentByEntity: false,
			Content: cardText, Kind: "appointment", AppointmentID: &appt.ID,
		}
		return tx.Omit(clause.Associations).Create(&card).Error
	})
	if err != nil {
		if isIdemViolation(err) {
			return nil, err // CreateAppointment devuelve la cita que ya existe con esa clave
		}
		if isOverlapViolation(err) || isRetryableConflict(err) {
			return nil, &SlotTakenError{Alternatives: s.alternatives(st, hrs, entityID, start, dur, nil)}
		}
		return nil, err
	}

	s.sendCard(&card, customerID, e.OwnerID)
	s.broadcast(appt.ID, customerID, e.OwnerID)
	if !review {
		s.notify(customerID, e.Name, "Cita confirmada: "+formatWhen(start), apptData("appointment_confirmed", &appt), "", false)
	}
	if instant {
		who := "Un cliente"
		var u models.User
		if s.db.Select("name").First(&u, "id = ?", customerID).Error == nil && u.Name != "" {
			who = u.Name
		}
		title, body := "Nueva solicitud de cita", fmt.Sprintf("%s pide %s", who, formatWhen(start))
		if !review {
			title, body = "Nueva cita", fmt.Sprintf("%s reservó %s", who, formatWhen(start))
		}
		s.notify(e.OwnerID, title, body, apptData("appointment_request", &appt), "appointment_request", false)
	}
	return s.load(appt.ID)
}

func (s *AgendaService) alternatives(st models.AgendaSettings, hrs []agenda.Hours, entityID uuid.UUID, target time.Time, dur int, daySlots []time.Time) []time.Time {
	if daySlots == nil {
		daySlots, _ = s.slotsForDay(st, hrs, entityID, target, dur)
	}
	alts := agenda.Closest(daySlots, target, 3)
	for i := 1; len(alts) < 3 && i <= 3; i++ {
		next, err := s.slotsForDay(st, hrs, entityID, target.AddDate(0, 0, i), dur)
		if err == nil {
			alts = append(alts, agenda.Closest(next, target, 3-len(alts))...)
		}
	}
	return alts
}

// sendCard delivers the appointment card message to both parties' open chats.
func (s *AgendaService) sendCard(msg *models.Message, users ...uuid.UUID) {
	if s.rt == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	seen := map[uuid.UUID]bool{}
	for _, u := range users {
		if seen[u] {
			continue
		}
		seen[u] = true
		s.rt.SendFrame(u, data)
	}
}

// ---------- state changes ----------

func (s *AgendaService) loadFor(userID, id uuid.UUID) (*models.Appointment, entityRef, error) {
	a, err := s.load(id)
	if err != nil {
		return nil, entityRef{}, err
	}
	e, err := s.entity(a.EntityID)
	if err != nil {
		return nil, e, ErrApptNotFound
	}
	isCustomer := a.CustomerID != nil && *a.CustomerID == userID
	if !isCustomer && e.OwnerID != userID {
		return nil, e, ErrApptForbidden
	}
	return a, e, nil
}

func (s *AgendaService) requireOwnerOf(userID, id uuid.UUID) (*models.Appointment, entityRef, error) {
	a, e, err := s.loadFor(userID, id)
	if err != nil {
		return nil, e, err
	}
	if e.OwnerID != userID || a.Kind != models.AppointmentBooking {
		return nil, e, ErrApptForbidden
	}
	return a, e, nil
}

func (s *AgendaService) GetAppointment(userID, id uuid.UUID) (*models.Appointment, error) {
	a, _, err := s.loadFor(userID, id)
	return a, err
}

// Accept confirms a pending request (business only).
func (s *AgendaService) Accept(ownerID, id uuid.UUID) (*models.Appointment, error) {
	a, e, err := s.requireOwnerOf(ownerID, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	deadline := a.EndsAt.Add(closingDeadlineAfterEnd)
	res := s.db.Model(&models.Appointment{}).
		Where("id = ? AND status = ? AND expires_at > ?", id, models.ApptRequested, now).
		Updates(map[string]interface{}{"status": models.ApptConfirmed, "responded_at": now, "closing_deadline": deadline})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrBadState
	}
	if a.CustomerID != nil {
		s.notify(*a.CustomerID, e.Name, "Cita confirmada: "+formatWhen(a.StartsAt), apptData("appointment_confirmed", a), "", false)
		s.broadcast(id, *a.CustomerID, ownerID)
	}
	return s.load(id)
}

// DeclineInput is the business's rejection.
type DeclineInput struct {
	ReasonCode string      `json:"reason_code"`
	Note       string      `json:"note"`
	Propose    []time.Time `json:"propose"`
}

func (s *AgendaService) Decline(ownerID, id uuid.UUID, in DeclineInput) (*models.Appointment, error) {
	if _, ok := declineReasons[in.ReasonCode]; !ok {
		return nil, &InvalidError{"reason_code debe ser no_space, no_service u other"}
	}
	a, e, err := s.requireOwnerOf(ownerID, id)
	if err != nil {
		return nil, err
	}
	// Keep only proposals that are really free for this appointment's length.
	var kept []time.Time
	if len(in.Propose) > 0 {
		st, err := s.settings(a.EntityID)
		if err != nil {
			return nil, err
		}
		hrs, err := s.hours(a.EntityID)
		if err != nil {
			return nil, err
		}
		dur := int(a.EndsAt.Sub(a.StartsAt) / time.Minute)
		cache := map[string][]time.Time{}
		for _, p := range in.Propose {
			if len(kept) == 3 {
				break
			}
			p = p.In(nowCO().Location()).Truncate(time.Minute)
			key := p.Format("2006-01-02")
			slots, ok := cache[key]
			if !ok {
				slots, _ = s.slotsForDay(st, hrs, a.EntityID, p, dur)
				cache[key] = slots
			}
			for _, sl := range slots {
				if sl.Equal(p) {
					kept = append(kept, p)
					break
				}
			}
		}
	}
	proposed := ""
	if len(kept) > 0 {
		b, _ := json.Marshal(kept)
		proposed = string(b)
	}
	now := time.Now()
	res := s.db.Model(&models.Appointment{}).
		Where("id = ? AND status = ?", id, models.ApptRequested).
		Updates(map[string]interface{}{
			"status": models.ApptDeclined, "responded_at": now,
			"decline_reason": in.ReasonCode, "decline_note": strings.TrimSpace(in.Note), "proposed_slots": proposed,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrBadState
	}
	if a.CustomerID != nil {
		body := declineReasons[in.ReasonCode]
		if len(kept) > 0 {
			body += ". Te propone otras horas"
		}
		s.notify(*a.CustomerID, e.Name+" no puede atenderte", body, apptData("appointment_declined", a), "", false)
		s.broadcast(id, *a.CustomerID, ownerID)
	}
	return s.load(id)
}

// Cancel is either side cancelling a pending or confirmed appointment.
func (s *AgendaService) Cancel(userID, id uuid.UUID) (*models.Appointment, error) {
	a, e, err := s.loadFor(userID, id)
	if err != nil {
		return nil, err
	}
	if a.Kind != models.AppointmentBooking {
		return nil, ErrBadState
	}
	by := "business"
	if a.CustomerID != nil && *a.CustomerID == userID {
		by = "customer"
	}
	now := time.Now()
	late := a.Status == models.ApptConfirmed && a.StartsAt.Sub(now) < lateCancelWindow
	res := s.db.Model(&models.Appointment{}).
		Where("id = ? AND status IN ?", id, []models.AppointmentStatus{models.ApptRequested, models.ApptConfirmed}).
		Updates(map[string]interface{}{"status": models.ApptCancelled, "cancelled_by": by, "cancelled_at": now, "cancelled_late": late})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrBadState
	}
	other := e.OwnerID
	title := "Cita cancelada"
	if by == "business" && a.CustomerID != nil {
		other = *a.CustomerID
		title = e.Name + " canceló la cita"
	} else if a.CustomerID != nil {
		var u models.User
		if s.db.Select("name").First(&u, "id = ?", *a.CustomerID).Error == nil && u.Name != "" {
			title = u.Name + " canceló la cita"
		}
	}
	s.notify(other, title, "Era "+formatWhen(a.StartsAt), apptData("appointment_cancelled", a), "", false)
	if a.CustomerID != nil {
		s.broadcast(id, *a.CustomerID, e.OwnerID)
	}
	return s.load(id)
}

// Outcome records one side's answer to "¿se dio la cita?".
func (s *AgendaService) Outcome(userID, id uuid.UUID, attended bool) (*models.Appointment, error) {
	a, e, err := s.loadFor(userID, id)
	if err != nil {
		return nil, err
	}
	if a.Kind != models.AppointmentBooking || a.Status != models.ApptConfirmed || time.Now().Before(a.EndsAt) {
		return nil, ErrBadState
	}
	col := "business_attended"
	if a.CustomerID != nil && *a.CustomerID == userID {
		col = "customer_attended"
	}
	res := s.db.Model(&models.Appointment{}).Where("id = ? AND status = ?", id, models.ApptConfirmed).Update(col, attended)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrBadState
	}
	s.resolveOutcome(id, false)
	if a.CustomerID != nil {
		s.broadcast(id, *a.CustomerID, e.OwnerID)
	}
	return s.load(id)
}

// resolveOutcome closes the appointment when the answers allow it.
func (s *AgendaService) resolveOutcome(id uuid.UUID, final bool) {
	var a models.Appointment
	if err := s.db.Select("id", "status", "customer_attended", "business_attended").First(&a, "id = ?", id).Error; err != nil {
		return
	}
	if a.Status != models.ApptConfirmed {
		return
	}
	next := agenda.Outcome(a.CustomerAttended, a.BusinessAttended, final)
	if next == "" {
		return
	}
	s.db.Model(&models.Appointment{}).Where("id = ? AND status = ?", id, models.ApptConfirmed).Update("status", next)
}

// ---------- agenda view and blocks ----------

// BusinessView is the owner's agenda: everything in the range plus every
// pending request, whatever its date.
func (s *AgendaService) BusinessView(ownerID, entityID uuid.UUID, from, to time.Time) ([]models.Appointment, error) {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return nil, err
	}
	var rows []models.Appointment
	err := s.db.Preload("Customer").Preload("Service").
		Where("entity_id = ? AND ((starts_at < ? AND ends_at > ? AND status IN ?) OR status = ?)",
			entityID, to, from,
			[]models.AppointmentStatus{models.ApptRequested, models.ApptConfirmed, models.ApptBlocked, models.ApptCompleted, models.ApptNoShow, models.ApptDisputed},
			models.ApptRequested).
		Order("starts_at ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	hrs, _ := s.hours(entityID)
	for i := range rows {
		hydrate(&rows[i])
		if rows[i].Status == models.ApptConfirmed && len(hrs) > 0 {
			rows[i].OutOfHours = !agenda.IsOpenAt(hrs, rows[i].StartsAt.In(nowCO().Location()))
		}
	}
	return rows, nil
}

// MyAppointments lists a customer's own appointments, newest first.
func (s *AgendaService) MyAppointments(userID uuid.UUID) ([]models.Appointment, error) {
	var rows []models.Appointment
	err := s.db.Preload("Entity").Preload("Service").
		Where("customer_id = ?", userID).Order("starts_at DESC").Limit(100).Find(&rows).Error
	for i := range rows {
		hydrate(&rows[i])
	}
	return rows, err
}

// CreateBlock closes a range of the agenda by hand. With live appointments in
// the way it fails with OverlapsBookingError unless force is set, in which
// case those appointments are cancelled (and their customers notified).
func (s *AgendaService) CreateBlock(ownerID, entityID uuid.UUID, start, end time.Time, force bool) (*models.Appointment, error) {
	e, err := s.requireOwner(ownerID, entityID)
	if err != nil {
		return nil, err
	}
	start = start.In(nowCO().Location()).Truncate(time.Minute)
	end = end.In(nowCO().Location()).Truncate(time.Minute)
	if !end.After(start) || end.Sub(start) > 24*time.Hour {
		return nil, &InvalidError{"el bloqueo debe terminar después de empezar y durar máximo 24 horas"}
	}

	var conflicts []models.Appointment
	if err := s.db.Preload("Customer").
		Where("entity_id = ? AND kind = ? AND status IN ? AND starts_at < ? AND blocks_until > ?",
			entityID, models.AppointmentBooking, []models.AppointmentStatus{models.ApptRequested, models.ApptConfirmed}, end, start).
		Find(&conflicts).Error; err != nil {
		return nil, err
	}
	if len(conflicts) > 0 && !force {
		return nil, &OverlapsBookingError{Appointments: conflicts}
	}

	block := models.Appointment{
		EntityID: entityID, Kind: models.AppointmentBlock, StartsAt: start, EndsAt: end, BlocksUntil: end, Status: models.ApptBlocked,
	}
	now := time.Now()
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for _, c := range conflicts {
			if err := tx.Model(&models.Appointment{}).Where("id = ? AND status IN ?", c.ID,
				[]models.AppointmentStatus{models.ApptRequested, models.ApptConfirmed}).
				Updates(map[string]interface{}{"status": models.ApptCancelled, "cancelled_by": "business", "cancelled_at": now}).Error; err != nil {
				return err
			}
		}
		return tx.Omit(clause.Associations).Create(&block).Error
	})
	if err != nil {
		if isOverlapViolation(err) {
			return nil, &InvalidError{"ya hay un bloqueo en ese horario"}
		}
		return nil, err
	}
	for i := range conflicts {
		c := &conflicts[i]
		if c.CustomerID != nil {
			s.notify(*c.CustomerID, e.Name+" canceló la cita", "Era "+formatWhen(c.StartsAt), apptData("appointment_cancelled", c), "", false)
			s.broadcast(c.ID, *c.CustomerID, ownerID)
		}
	}
	return &block, nil
}

func (s *AgendaService) DeleteBlock(ownerID, entityID, blockID uuid.UUID) error {
	if _, err := s.requireOwner(ownerID, entityID); err != nil {
		return err
	}
	res := s.db.Where("id = ? AND entity_id = ? AND kind = ?", blockID, entityID, models.AppointmentBlock).Delete(&models.Appointment{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrApptNotFound
	}
	return nil
}
