package handlers

import (
	"testing"
	"time"

	"empre_backend/internal/models"
)

func TestOpeningInfo(t *testing.T) {
	zone := time.FixedZone("America/Bogota", -5*60*60)
	now := time.Date(2026, 10, 15, 21, 0, 0, 0, zone) // jueves 9 pm
	hours := []models.BusinessHour{
		{Weekday: 4, OpenTime: "08:00", CloseTime: "20:00"},
		{Weekday: 5, OpenTime: "09:00", CloseTime: "18:00"},
	}

	if c, n := openingInfo(nil, false, now); c != "" || n != nil {
		t.Fatalf("sin horario no dice nada: %q %+v", c, n)
	}
	// Abierto: solo la hora de cierre, sin próxima apertura.
	open := time.Date(2026, 10, 15, 12, 0, 0, 0, zone)
	if c, n := openingInfo(hours, true, open); c != "20:00" || n != nil {
		t.Fatalf("abierto: %q %+v", c, n)
	}
	// Cerrado: la próxima apertura (mañana 9:00), sin hora de cierre.
	c, n := openingInfo(hours, false, now)
	if c != "" || n == nil || n.DaysAhead != 1 || n.Weekday != 5 || n.Time != "09:00" {
		t.Fatalf("cerrado: %q %+v", c, n)
	}
	// Cerrado y sin ningún día de apertura: nada.
	if c, n := openingInfo([]models.BusinessHour{{Weekday: 4, Closed: true}}, false, now); c != "" || n != nil {
		t.Fatalf("nunca abre: %q %+v", c, n)
	}
}
