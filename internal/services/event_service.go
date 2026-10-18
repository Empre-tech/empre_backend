package services

import (
	"errors"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Medición mínima (E1): qué pasa en la app sin identificar a nadie. Todo lo que entra se valida contra una
// lista cerrada de eventos y de datos por evento; lo demás se descarta.

const (
	// MaxEventsPerBatch es el tope de eventos por petición.
	MaxEventsPerBatch = 20
	maxPropString     = 80
	maxQueryLen       = 60
	maxPropNumber     = 100000
	omittedQuery      = "[omitida]"
)

type propKind int

const (
	propString propKind = iota
	propNumber
	propBool
)

// EventSpec define un evento permitido: si exige negocio y qué datos acepta (clave → tipo).
type EventSpec struct {
	NeedsEntity bool
	Props       map[string]propKind
}

// EventSpecs son los únicos eventos que se aceptan.
var EventSpecs = map[string]EventSpec{
	"search_submitted":  {Props: map[string]propKind{"query": propString, "results": propNumber, "scope": propString}},
	"search_no_results": {Props: map[string]propKind{"query": propString, "scope": propString}},
	"filter_applied":    {Props: map[string]propKind{"filter": propString}},
	"profile_opened":    {NeedsEntity: true, Props: map[string]propKind{"origin": propString}},
	"offer_viewed":      {NeedsEntity: true, Props: map[string]propKind{"offer_id": propString}},
	"promotion_viewed":  {NeedsEntity: true, Props: map[string]propKind{"promotion_id": propString}},
	"contact_started":   {NeedsEntity: true},
	"directions_tapped": {NeedsEntity: true, Props: map[string]propKind{"origin": propString}},
	"follow_toggled":    {NeedsEntity: true, Props: map[string]propKind{"following": propBool}},
	// «Pregúntale»: qué se preguntó (la intención, nunca el texto) y si había dato para responder.
	"question_asked":  {NeedsEntity: true, Props: map[string]propKind{"intent": propString, "answered": propBool}},
	"booking_started": {NeedsEntity: true, Props: map[string]propKind{"origin": propString}},
}

// EventInput es un evento tal como llega de la app.
type EventInput struct {
	Name     string         `json:"name"`
	EntityID string         `json:"entity_id"`
	Props    map[string]any `json:"props"`
}

var (
	anonIDRe  = regexp.MustCompile(`^[A-Za-z0-9-]{16,64}$`)
	digitsRe  = regexp.MustCompile(`\d{7,}`)
	spacesRe  = regexp.MustCompile(`\s+`)
	ErrAnonID = errors.New("anon_id inválido")
)

// NormalizeQuery deja la consulta en minúsculas, sin espacios repetidos y de hasta 60 letras. Si parece contener
// un dato personal (correo o una cifra larga, como un teléfono), no se guarda: queda «[omitida]».
func NormalizeQuery(q string) string {
	q = spacesRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(q)), " ")
	if q == "" {
		return ""
	}
	if strings.Contains(q, "@") || digitsRe.MatchString(strings.ReplaceAll(strings.ReplaceAll(q, " ", ""), "-", "")) {
		return omittedQuery
	}
	if utf8.RuneCountInString(q) > maxQueryLen {
		q = string([]rune(q)[:maxQueryLen])
	}
	return q
}

// ValidateEvent convierte un evento recibido en uno guardable, o dice por qué se rechaza. Los datos que el
// evento no admite se descartan en silencio; un dato admitido con tipo equivocado rechaza el evento.
func ValidateEvent(anonID string, in EventInput) (models.Event, error) {
	if !anonIDRe.MatchString(anonID) {
		return models.Event{}, ErrAnonID
	}
	spec, ok := EventSpecs[in.Name]
	if !ok {
		return models.Event{}, errors.New("evento no permitido")
	}
	ev := models.Event{Name: in.Name, AnonID: anonID}
	if in.EntityID != "" {
		id, err := uuid.Parse(in.EntityID)
		if err != nil {
			return models.Event{}, errors.New("entity_id inválido")
		}
		ev.EntityID = &id
	} else if spec.NeedsEntity {
		return models.Event{}, errors.New("el evento necesita entity_id")
	}
	if len(in.Props) > 0 {
		props := map[string]any{}
		for key, kind := range spec.Props {
			raw, present := in.Props[key]
			if !present || raw == nil {
				continue
			}
			switch kind {
			case propString:
				s, ok := raw.(string)
				if !ok {
					return models.Event{}, errors.New("dato " + key + " debe ser texto")
				}
				if key == "query" {
					s = NormalizeQuery(s)
				} else {
					s = strings.TrimSpace(s)
				}
				if utf8.RuneCountInString(s) > maxPropString {
					s = string([]rune(s)[:maxPropString])
				}
				if s != "" {
					props[key] = s
				}
			case propNumber:
				n, ok := raw.(float64)
				if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > maxPropNumber {
					return models.Event{}, errors.New("dato " + key + " debe ser un número entre 0 y 100000")
				}
				props[key] = n
			case propBool:
				b, ok := raw.(bool)
				if !ok {
					return models.Event{}, errors.New("dato " + key + " debe ser verdadero o falso")
				}
				props[key] = b
			}
		}
		if len(props) > 0 {
			ev.Props = props
		}
	}
	return ev, nil
}

// EventService guarda eventos y los resume.
type EventService struct {
	DB *gorm.DB
}

func NewEventService(db *gorm.DB) *EventService {
	return &EventService{DB: db}
}

// IngestResult dice cuántos eventos de la petición se guardaron y cuántos se rechazaron.
type IngestResult struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// Ingest valida y guarda un lote. Un evento inválido no impide guardar los demás.
func (s *EventService) Ingest(anonID string, inputs []EventInput, now time.Time) (IngestResult, error) {
	var res IngestResult
	events := make([]models.Event, 0, len(inputs))
	for _, in := range inputs {
		ev, err := ValidateEvent(anonID, in)
		if err != nil {
			res.Rejected++
			continue
		}
		ev.CreatedAt = now
		events = append(events, ev)
	}
	if len(events) > 0 {
		if err := s.DB.Create(&events).Error; err != nil {
			return IngestResult{}, err
		}
	}
	res.Accepted = len(events)
	return res, nil
}

// EventCount es el conteo de un evento y de cuántas instalaciones distintas lo generaron.
type EventCount struct {
	Name   string `json:"name"`
	Events int64  `json:"events"`
	Users  int64  `json:"installs"`
}

// Summary cuenta los eventos desde `since`, por nombre.
func (s *EventService) Summary(since time.Time) ([]EventCount, error) {
	var rows []EventCount
	err := s.DB.Model(&models.Event{}).
		Select("name, COUNT(*) AS events, COUNT(DISTINCT anon_id) AS users").
		Where("created_at >= ?", since).
		Group("name").Order("events DESC").Scan(&rows).Error
	return rows, err
}

// PurgeOlderThan borra los eventos anteriores a `before` (retención). Devuelve cuántos.
func (s *EventService) PurgeOlderThan(before time.Time) (int64, error) {
	res := s.DB.Where("created_at < ?", before).Delete(&models.Event{})
	return res.RowsAffected, res.Error
}
