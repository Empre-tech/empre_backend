package utils

import (
	"time"

	"empre_backend/internal/models"
)

// colombiaZone is Colombia's fixed UTC-5 offset (no DST). Using a fixed
// zone instead of time.LoadLocation("America/Bogota") avoids depending on
// the IANA tzdata being present in the deployment image.
var colombiaZone = time.FixedZone("America/Bogota", -5*60*60)

// cartagenaZone: alias usado por las pruebas de la rama performance-testing.
var cartagenaZone = colombiaZone

// NowInColombia returns the current time in Colombia's timezone (same
// offset nationwide, used for every business regardless of its city), the
// reference clock for every "abierto ahora" computation.
func NowInColombia() time.Time {
	return time.Now().In(colombiaZone)
}

// NowInCartagena es un alias de NowInColombia: la rama de pruebas de rendimiento
// usa este nombre; Colombia tiene un único huso (UTC-5) y ambos son idénticos.
func NowInCartagena() time.Time {
	return NowInColombia()
}

// IsOpenNow reports whether a business is open right now, given its weekly
// schedule (one row per weekday, Go's time.Weekday numbering: 0=domingo).
// Handles a day's hours crossing midnight (e.g. abre 18:00, cierra 02:00) by
// also checking whether yesterday's hours are still running into today.
func IsOpenNow(hours []models.BusinessHour, now time.Time) bool {
	today := int(now.Weekday())
	yesterday := (today + 6) % 7
	nowTime := now.Format("15:04")

	var todayHours, yesterdayHours *models.BusinessHour
	for i := range hours {
		switch hours[i].Weekday {
		case today:
			todayHours = &hours[i]
		case yesterday:
			yesterdayHours = &hours[i]
		}
	}

	if todayHours != nil {
		if todayHours.Is24h {
			return true
		}
		if !todayHours.Closed && todayHours.OpenTime != "" && todayHours.CloseTime != "" {
			if todayHours.CloseTime > todayHours.OpenTime {
				// Rango dentro del mismo día.
				if nowTime >= todayHours.OpenTime && nowTime < todayHours.CloseTime {
					return true
				}
			} else if nowTime >= todayHours.OpenTime {
				// Cruza la medianoche: abierto desde OpenTime hasta el final del día.
				return true
			}
		}
	}

	if yesterdayHours != nil && !yesterdayHours.Closed && !yesterdayHours.Is24h &&
		yesterdayHours.OpenTime != "" && yesterdayHours.CloseTime != "" &&
		yesterdayHours.CloseTime <= yesterdayHours.OpenTime {
		// El horario de ayer cruzó la medianoche: sigue abierto hasta CloseTime.
		if nowTime < yesterdayHours.CloseTime {
			return true
		}
	}

	return false
}

// ClosesAt dice a qué hora cierra hoy un negocio que está abierto ahora
// ("HH:MM"), para mostrar «Abierto · hasta las 8:00 pm». Devuelve "24h" si
// atiende las 24 horas y "" si está cerrado o no tiene horario.
func ClosesAt(hours []models.BusinessHour, now time.Time) string {
	today := int(now.Weekday())
	yesterday := (today + 6) % 7
	nowTime := now.Format("15:04")

	for i := range hours {
		h := hours[i]
		if h.Weekday == yesterday && !h.Closed && !h.Is24h && h.OpenTime != "" && h.CloseTime != "" &&
			h.CloseTime <= h.OpenTime && nowTime < h.CloseTime {
			// El horario de ayer cruzó la medianoche y sigue corriendo.
			return h.CloseTime
		}
	}
	for i := range hours {
		h := hours[i]
		if h.Weekday != today || h.Closed {
			continue
		}
		if h.Is24h {
			return "24h"
		}
		if h.OpenTime == "" || h.CloseTime == "" || nowTime < h.OpenTime {
			return ""
		}
		if h.CloseTime > h.OpenTime && nowTime >= h.CloseTime {
			return ""
		}
		return h.CloseTime
	}
	return ""
}

// NextOpen es la próxima apertura de un negocio que está cerrado ahora.
type NextOpen struct {
	// DaysAhead: 0 = hoy más tarde, 1 = mañana, 2…7 = dentro de tantos días.
	DaysAhead int
	// Weekday del día de apertura (0=domingo).
	Weekday int
	// Time de apertura, "HH:MM" ("00:00" si ese día atiende las 24 horas).
	Time string
}

// NextOpening dice cuándo vuelve a abrir un negocio que está cerrado ahora, mirando hoy y los 7 días siguientes.
// ok es false sin horario o si no abre ningún día. Quien llama decide si el negocio está cerrado: con el negocio
// abierto el resultado no tiene sentido.
func NextOpening(hours []models.BusinessHour, now time.Time) (next NextOpen, ok bool) {
	today := int(now.Weekday())
	nowTime := now.Format("15:04")
	for off := 0; off <= 7; off++ {
		wd := (today + off) % 7
		for i := range hours {
			h := hours[i]
			if h.Weekday != wd || h.Closed {
				continue
			}
			if h.Is24h {
				if off == 0 {
					continue // hoy ya está abierto todo el día
				}
				return NextOpen{DaysAhead: off, Weekday: wd, Time: "00:00"}, true
			}
			if h.OpenTime == "" || h.CloseTime == "" {
				continue
			}
			if off == 0 && nowTime >= h.OpenTime {
				continue // hoy ya abrió (y ya cerró): sigue mañana
			}
			return NextOpen{DaysAhead: off, Weekday: wd, Time: h.OpenTime}, true
		}
	}
	return NextOpen{}, false
}
