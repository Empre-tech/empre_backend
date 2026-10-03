package utils

import (
	"time"

	"empre_backend/internal/models"
)

// colombiaZone is Colombia's fixed UTC-5 offset (no DST). Using a fixed
// zone instead of time.LoadLocation("America/Bogota") avoids depending on
// the IANA tzdata being present in the deployment image.
var colombiaZone = time.FixedZone("America/Bogota", -5*60*60)

// NowInColombia returns the current time in Colombia's timezone (same
// offset nationwide, used for every business regardless of its city), the
// reference clock for every "abierto ahora" computation.
func NowInColombia() time.Time {
	return time.Now().In(colombiaZone)
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
