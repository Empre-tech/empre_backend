package agenda

import "time"

// Recepción de solicitudes y avisos al negocio (CONVERSACIONES_Y_AGENDA.md §9b). Reglas puras, sin base de datos.

// Modos de recepción.
const (
	IntakeManual = "manual" // el dueño revisa cada solicitud
	IntakeRules  = "rules"  // se confirman solas salvo que una regla pida revisión (por defecto)
	IntakeAuto   = "auto"   // hora libre = cita confirmada
)

// Intake son el modo y las reglas del negocio. Cero o falso = regla apagada.
type Intake struct {
	Mode             string
	ReviewNoShows    int  // revisar si el cliente tiene al menos N faltas con este negocio
	ReviewFirstVisit bool // revisar la primera cita del cliente con este negocio
	ReviewNoticeMin  int  // revisar si pide con menos de N minutos de anticipación
}

// History es lo que ya pasó entre ese cliente y ese negocio.
type History struct {
	Completed int
	NoShows   int
}

// NeedsReview dice si la solicitud espera al dueño y por qué. Un modo desconocido se trata como manual (lo seguro).
func NeedsReview(in Intake, h History, start, now time.Time) (bool, string) {
	switch in.Mode {
	case IntakeAuto:
		return false, ""
	case IntakeRules:
	default:
		return true, "manual"
	}
	if in.ReviewNoShows > 0 && h.NoShows >= in.ReviewNoShows {
		return true, "faltas"
	}
	if in.ReviewFirstVisit && h.Completed == 0 && h.NoShows == 0 {
		return true, "primera visita"
	}
	if in.ReviewNoticeMin > 0 && start.Sub(now) < time.Duration(in.ReviewNoticeMin)*time.Minute {
		return true, "poca anticipación"
	}
	return false, ""
}

// Silencio de avisos al negocio.
const (
	QuietHours  = "hours"  // fuera del horario publicado (por defecto)
	QuietCustom = "custom" // horas elegidas por el dueño
	QuietOff    = "off"    // nunca
)

// Quiet es la preferencia de silencio del negocio. From y To son "HH:MM"; From > To cruza la medianoche.
type Quiet struct {
	Mode     string
	From, To string
}

// InQuiet dice si en t no se le avisa al negocio (lo pendiente le llega en un resumen al salir del silencio).
// Con «fuera de mi horario» y sin horario publicado se usa el silencio fijo de 9:00 pm a 7:00 am.
func InQuiet(q Quiet, hours []Hours, t time.Time) bool {
	switch q.Mode {
	case QuietOff:
		return false
	case QuietCustom:
		from, ok1 := parseClock(q.From)
		to, ok2 := parseClock(q.To)
		if !ok1 || !ok2 || from == to {
			return false
		}
		m := time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute
		if from < to {
			return m >= from && m < to
		}
		return m >= from || m < to
	default:
		if len(hours) == 0 {
			return InSilence(t)
		}
		return !IsOpenAt(hours, t)
	}
}

// ValidClock dice si s es "HH:MM" válido.
func ValidClock(s string) bool {
	_, ok := parseClock(s)
	return ok
}
