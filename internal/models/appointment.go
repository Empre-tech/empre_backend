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
	LunchStart   string    `json:"lunch_start"`
	LunchEnd     string    `json:"lunch_end"`
	BufferMin    int       `gorm:"not null;default:0" json:"buffer_min"`
	MinNoticeMin int       `gorm:"not null;default:45" json:"min_notice_min"`
	HorizonDays  int       `gorm:"not null;default:14" json:"horizon_days"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (AgendaSettings) TableName() string { return "agenda_settings" }

// AppointmentService is a service with its own duration (Corte 30, Tinte 60).
type AppointmentService struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EntityID    uuid.UUID `gorm:"type:uuid;not null;index" json:"entity_id"`
	Name        string    `gorm:"not null" json:"name"`
	DurationMin int       `gorm:"not null" json:"duration_min"`
	Active      bool      `gorm:"default:true" json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (AppointmentService) TableName() string { return "appointment_services" }

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

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Filled by the API for responses (not stored).
	OutOfHours bool `gorm:"-" json:"out_of_hours,omitempty"`
	// Alternative start times offered when the business declined (decoded from ProposedSlots).
	Proposed []time.Time `gorm:"-" json:"proposed_slots,omitempty"`

	Entity   Entity              `gorm:"foreignKey:EntityID" json:"entity,omitempty"`
	Customer *User               `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Service  *AppointmentService `gorm:"foreignKey:ServiceID" json:"service,omitempty"`
}

func (Appointment) TableName() string { return "appointments" }

// Active reports whether the row still occupies its time range.
func (a *Appointment) Active() bool {
	return a.Status == ApptRequested || a.Status == ApptConfirmed || a.Status == ApptBlocked
}
