package handlers

import (
	"errors"
	"net/http"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AgendaHandler struct {
	service *services.AgendaService
}

func NewAgendaHandler(service *services.AgendaService) *AgendaHandler {
	return &AgendaHandler{service: service}
}

func currentUser(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get("userID")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return uuid.Nil, false
	}
	return id, true
}

func uuidParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return uuid.Nil, false
	}
	return id, true
}

// fail maps service errors to HTTP responses.
func (h *AgendaHandler) fail(c *gin.Context, err error) {
	var invalid *services.InvalidError
	var taken *services.SlotTakenError
	var overlaps *services.OverlapsBookingError
	switch {
	case errors.As(err, &taken):
		c.JSON(http.StatusConflict, gin.H{"error": "slot_taken", "alternatives": taken.Alternatives})
	case errors.As(err, &overlaps):
		c.JSON(http.StatusConflict, gin.H{"error": "overlaps_booking", "appointments": overlaps.Appointments})
	case errors.As(err, &invalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid", "message": invalid.Msg})
	case errors.Is(err, services.ErrApptNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, services.ErrApptForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, services.ErrEntityHidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "entity_hidden"})
	case errors.Is(err, services.ErrAgendaDisabled):
		c.JSON(http.StatusConflict, gin.H{"error": "agenda_disabled"})
	case errors.Is(err, services.ErrBadState):
		c.JSON(http.StatusConflict, gin.H{"error": "bad_state"})
	case errors.Is(err, services.ErrTooManyPending):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too_many_pending"})
	case errors.Is(err, services.ErrCustomerBusy):
		c.JSON(http.StatusConflict, gin.H{"error": "customer_busy"})
	case errors.Is(err, services.ErrOwnBusiness):
		c.JSON(http.StatusBadRequest, gin.H{"error": "own_business"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// Summary: GET /entities/:id/agenda/summary — the profile's availability signal.
func (h *AgendaHandler) Summary(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	out, err := h.service.Summary(user, entityID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

// Availability: GET /entities/:id/agenda/availability?date=YYYY-MM-DD&service_id=
func (h *AgendaHandler) Availability(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	loc := time.FixedZone("America/Bogota", -5*60*60)
	day, err := time.ParseInLocation("2006-01-02", c.Query("date"), loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date must be YYYY-MM-DD"})
		return
	}
	var serviceID *uuid.UUID
	if raw := c.Query("service_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
			return
		}
		serviceID = &id
	}
	slots, err := h.service.Availability(user, entityID, day, serviceID)
	if err != nil {
		h.fail(c, err)
		return
	}
	if slots == nil {
		slots = []time.Time{}
	}
	c.JSON(http.StatusOK, gin.H{"date": c.Query("date"), "slots": slots})
}

type createAppointmentRequest struct {
	EntityID  uuid.UUID  `json:"entity_id" binding:"required"`
	ServiceID *uuid.UUID `json:"service_id"`
	StartsAt  time.Time  `json:"starts_at" binding:"required"`
	Note      string     `json:"note"`
}

// Create: POST /appointments
func (h *AgendaHandler) Create(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	var req createAppointmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Note) > 300 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "note too long"})
		return
	}
	appt, err := h.service.CreateAppointment(user, req.EntityID, req.ServiceID, req.StartsAt, req.Note)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, appt)
}

// Mine: GET /appointments/mine
func (h *AgendaHandler) Mine(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	rows, err := h.service.MyAppointments(user)
	if err != nil {
		h.fail(c, err)
		return
	}
	if rows == nil {
		rows = []models.Appointment{}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// Get: GET /appointments/:id
func (h *AgendaHandler) Get(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	appt, err := h.service.GetAppointment(user, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, appt)
}

func (h *AgendaHandler) transition(c *gin.Context, do func(user, id uuid.UUID) (*models.Appointment, error)) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	id, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	appt, err := do(user, id)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, appt)
}

func (h *AgendaHandler) Accept(c *gin.Context) { h.transition(c, h.service.Accept) }
func (h *AgendaHandler) Cancel(c *gin.Context) { h.transition(c, h.service.Cancel) }

// Decline: POST /appointments/:id/decline
func (h *AgendaHandler) Decline(c *gin.Context) {
	var in services.DeclineInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.transition(c, func(user, id uuid.UUID) (*models.Appointment, error) {
		return h.service.Decline(user, id, in)
	})
}

// Outcome: POST /appointments/:id/outcome  {"attended": true}
func (h *AgendaHandler) Outcome(c *gin.Context) {
	var in struct {
		Attended *bool `json:"attended" binding:"required"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.transition(c, func(user, id uuid.UUID) (*models.Appointment, error) {
		return h.service.Outcome(user, id, *in.Attended)
	})
}

// BusinessView: GET /entities/:id/agenda?from=&to= (RFC3339; default: today)
func (h *AgendaHandler) BusinessView(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	loc := time.FixedZone("America/Bogota", -5*60*60)
	now := time.Now().In(loc)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, 1)
	if raw := c.Query("from"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "from must be RFC3339"})
			return
		}
		from = t
	}
	if raw := c.Query("to"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "to must be RFC3339"})
			return
		}
		to = t
	}
	if !to.After(from) || to.Sub(from) > 62*24*time.Hour {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid range"})
		return
	}
	rows, err := h.service.BusinessView(user, entityID, from, to)
	if err != nil {
		h.fail(c, err)
		return
	}
	if rows == nil {
		rows = []models.Appointment{}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// GetSettings: GET /entities/:id/agenda/settings
func (h *AgendaHandler) GetSettings(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	st, err := h.service.GetSettings(user, entityID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

// UpdateSettings: PUT /entities/:id/agenda/settings
func (h *AgendaHandler) UpdateSettings(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var in services.SettingsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st, err := h.service.UpdateSettings(user, entityID, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

// ListServices: GET /entities/:id/agenda/services
func (h *AgendaHandler) ListServices(c *gin.Context) {
	if _, ok := currentUser(c); !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	rows, err := h.service.ListServices(entityID, true)
	if err != nil {
		h.fail(c, err)
		return
	}
	if rows == nil {
		rows = []services.BookableService{}
	}
	c.JSON(http.StatusOK, gin.H{"data": rows})
}

// CreateService: POST /entities/:id/agenda/services
func (h *AgendaHandler) CreateService(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var in services.AgendaServiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	svc, err := h.service.CreateService(user, entityID, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, svc)
}

// UpdateService: PUT /entities/:id/agenda/services/:serviceId
func (h *AgendaHandler) UpdateService(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	serviceID, ok := uuidParam(c, "serviceId")
	if !ok {
		return
	}
	var in services.AgendaServiceInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	svc, err := h.service.UpdateService(user, entityID, serviceID, in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, svc)
}

// DeleteService: DELETE /entities/:id/agenda/services/:serviceId
func (h *AgendaHandler) DeleteService(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	serviceID, ok := uuidParam(c, "serviceId")
	if !ok {
		return
	}
	if err := h.service.DeleteService(user, entityID, serviceID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// CreateBlock: POST /entities/:id/agenda/blocks {"starts_at","ends_at","force"}
func (h *AgendaHandler) CreateBlock(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		StartsAt time.Time `json:"starts_at" binding:"required"`
		EndsAt   time.Time `json:"ends_at" binding:"required"`
		Force    bool      `json:"force"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	block, err := h.service.CreateBlock(user, entityID, in.StartsAt, in.EndsAt, in.Force)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, block)
}

// DeleteBlock: DELETE /entities/:id/agenda/blocks/:blockId
func (h *AgendaHandler) DeleteBlock(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		return
	}
	entityID, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	blockID, ok := uuidParam(c, "blockId")
	if !ok {
		return
	}
	if err := h.service.DeleteBlock(user, entityID, blockID); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
