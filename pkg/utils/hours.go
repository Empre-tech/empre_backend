package utils

import (
	"time"

	"empre_backend/internal/models"
)

// cartagenaZone is Colombia's fixed UTC-5 offset (no DST). Using a fixed
// zone instead of time.LoadLocation("America/Bogota") avoids depending on
// the IANA tzdata being present in the deployment image.
var cartagenaZone = time.FixedZone("America/Bogota", -5*60*60)

// NowInCartagena returns the current time in Cartagena's timezone, the
// reference clock for every "abierto ahora" computation.
func NowInCartagena() time.Time {
	return time.Now().In(cartagenaZone)
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
