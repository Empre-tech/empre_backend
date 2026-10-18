package models

import (
	"time"

	"github.com/google/uuid"
)

// AgendaSettings is the on/off switch and the weekly-rules configuration of
// the appointments module, one row per business. The weekly availability
// itself lives in BusinessHour (single source of truth for opening hours).
type AgendaSettings struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"entity_id"`
	Enabled  bool      `gorm:"default:false" json:"enabled"`
	// Grid of start times, and the duration of an appointment when the
	// business has not defined services (15/30/45/60).
	SlotMinutes int `gorm:"not null;default:30" json:"slot_minutes"`
	// "HH:MM" or empty.
	LunchStart   string `json:"lunch_start"`
	LunchEnd     string `json:"lunch_end"`
	BufferMin    int    `gorm:"not null;default:0" json:"buffer_min"`
	MinNoticeMin int    `gorm:"not null;default:45" json:"min_notice_min"`
	HorizonDays  int    `gorm:"not null;default:14" json:"horizon_days"`
	// Cómo recibe las solicitudes (§9b): manual | rules | auto. Por defecto, con reglas.
	IntakeMode       string `gorm:"type:varchar(10);not null;default:'rules'" json:"intake_mode"`
	ReviewNoShows    int    `gorm:"not null;default:2" json:"review_no_shows"` // 0 = apagada
	ReviewFirstVisit bool   `gorm:"not null;default:false" json:"review_first_visit"`
	ReviewNoticeMin  int    `gorm:"not null;default:120" json:"review_notice_min"` // 0 = apagada
	// Avisos al dueño, editables y según su horario: cada cuántos minutos llega el resumen (0 = al instante) y
	// cuándo no se le molesta (hours | custom | off).
	NotifyEvery  int        `gorm:"not null;default:60" json:"notify_every"`
	QuietMode    string     `gorm:"type:varchar(10);not null;default:'hours'" json:"quiet_mode"`
	QuietFrom    string     `json:"quiet_from"`
	QuietTo      string     `json:"quiet_to"`
	LastDigestAt *time.Time `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (AgendaSettings) TableName() string { return "agenda_settings" }

// Los servicios que se reservan son los del catálogo (models.ServiceItem con Bookable y DurationMin): una sola lista
// para el dueño (CONVERSACIONES_Y_AGENDA.md, D2).

type AppointmentKind string

const (
	AppointmentBooking AppointmentKind = "booking"
	AppointmentBlock   AppointmentKind = "block"
)

type AppointmentStatus string

const (
	ApptRequested AppointmentStatus = "requested"
	ApptConfirmed AppointmentStatus = "confirmed"
	ApptDeclined  AppointmentStatus = "declined"
	ApptExpired   AppointmentStatus = "expired"
	ApptCancelled AppointmentStatus = "cancelled"
	ApptCompleted AppointmentStatus = "completed"
	ApptNoShow    AppointmentStatus = "no_show"
	ApptDisputed  AppointmentStatus = "disputed"
	ApptBlocked   AppointmentStatus = "blocked"
)

// Appointment is a customer's booking OR a manual block of the business's
// agenda, in a single table so one database constraint keeps them from
// overlapping (see database.EnsureAgendaConstraints).
type Appointment struct {
	ID         uuid.UUID       `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID   uuid.UUID       `gorm:"type:uuid;not null;index:idx_appt_entity_start,priority:1" json:"entity_id"`
	Kind       AppointmentKind `gorm:"type:varchar(10);not null;default:'booking'" json:"kind"`
	CustomerID *uuid.UUID      `gorm:"type:uuid;index" json:"customer_id,omitempty"`
	ServiceID  *uuid.UUID      `gorm:"type:uuid" json:"service_id,omitempty"`

	StartsAt time.Time `gorm:"not null;index:idx_appt_entity_start,priority:2" json:"starts_at"`
	EndsAt   time.Time `gorm:"not null" json:"ends_at"`
	// EndsAt + the business's buffer at creation time: the range the exclusion
	// constraint protects.
	BlocksUntil time.Time         `gorm:"not null" json:"-"`
	Status      AppointmentStatus `gorm:"type:varchar(20);not null;index" json:"status"`

	CustomerNote  string `json:"customer_note,omitempty"`
	DeclineReason string `json:"decline_reason,omitempty"` // code: no_space | no_service | other
	DeclineNote   string `json:"decline_note,omitempty"`
	// Alternative start times proposed when declining (JSON array of RFC3339).
	ProposedSlots string `gorm:"type:text" json:"-"`

	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`

	CancelledBy   string     `gorm:"type:varchar(10)" json:"cancelled_by,omitempty"` // customer | business
	CancelledAt   *time.Time `json:"cancelled_at,omitempty"`
	CancelledLate bool       `gorm:"default:false" json:"cancelled_late"`

	CustomerAttended *bool      `json:"customer_attended,omitempty"`
	BusinessAttended *bool      `json:"business_attended,omitempty"`
	ClosingDeadline  *time.Time `json:"closing_deadline,omitempty"`

	// Marks that keep each reminder from repeating.
	HalfwayRemindedAt   *time.Time `gorm:"column:halfway_reminded_at" json:"-"`
	Reminded24hAt       *time.Time `gorm:"column:reminded24h_at" json:"-"`
	Reminded2hAt        *time.Time `gorm:"column:reminded2h_at" json:"-"`
	ClosingPromptedAt   *time.Time `gorm:"column:closing_prompted_at" json:"-"`
	ClosingReminded24At *time.Time `gorm:"column:closing_reminded24_at" json:"-"`

	// Clave del intento de reserva que manda la app: un reintento con la misma clave devuelve la misma cita.
	IdempotencyKey *string `gorm:"type:varchar(64)" json:"-"`
	// Cuándo se le avisó al dueño (directo o en un resumen). Nulo = pendiente de avisar.
	OwnerNotifiedAt *time.Time `json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Filled by the API for responses (not stored).
	OutOfHours bool `gorm:"-" json:"out_of_hours,omitempty"`
	// Alternative start times offered when the business declined (decoded from ProposedSlots).
	Proposed []time.Time `gorm:"-" json:"proposed_slots,omitempty"`

	Entity   Entity       `gorm:"foreignKey:EntityID" json:"entity,omitempty"`
	Customer *User        `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Service  *ServiceItem `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
}

func (Appointment) TableName() string { return "appointments" }

// Active reports whether the row still occupies its time range.
func (a *Appointment) Active() bool {
	return a.Status == ApptRequested || a.Status == ApptConfirmed || a.Status == ApptBlocked
}
