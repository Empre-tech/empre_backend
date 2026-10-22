package utils

import (
	"testing"
	"time"

	"empre_backend/internal/models"
)

// ClosesAt da la hora de cierre de hoy solo si el negocio está abierto ahora.
func TestClosesAt(t *testing.T) {
	// Jueves 15 de octubre de 2026 (Weekday 4).
	at := func(hh, mm int) time.Time { return time.Date(2026, 10, 15, hh, mm, 0, 0, cartagenaZone) }
	day := func(w int, open, close string) models.BusinessHour {
		return models.BusinessHour{Weekday: w, OpenTime: open, CloseTime: close}
	}
	cases := []struct {
		name  string
		hours []models.BusinessHour
		now   time.Time
		want  string
	}{
		{"abierto en el día", []models.BusinessHour{day(4, "08:00", "20:00")}, at(15, 0), "20:00"},
		{"antes de abrir", []models.BusinessHour{day(4, "08:00", "20:00")}, at(7, 0), ""},
		{"después de cerrar", []models.BusinessHour{day(4, "08:00", "20:00")}, at(20, 0), ""},
		{"24 horas", []models.BusinessHour{{Weekday: 4, Is24h: true}}, at(3, 0), "24h"},
		{"cerrado hoy", []models.BusinessHour{{Weekday: 4, Closed: true}}, at(12, 0), ""},
		{"cruza la medianoche, de noche", []models.BusinessHour{day(4, "18:00", "02:00")}, at(23, 0), "02:00"},
		{"cruza la medianoche, madrugada de ayer", []models.BusinessHour{day(3, "18:00", "02:00")}, at(1, 0), "02:00"},
		{"sin horario", nil, at(12, 0), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClosesAt(tc.hours, tc.now); got != tc.want {
				t.Errorf("ClosesAt = %q, se esperaba %q", got, tc.want)
			}
		})
	}
}

// NextOpening dice cuándo vuelve a abrir un negocio cerrado.
func TestNextOpening(t *testing.T) {
	// Jueves 15 de octubre de 2026 (Weekday 4).
	at := func(hh, mm int) time.Time { return time.Date(2026, 10, 15, hh, mm, 0, 0, cartagenaZone) }
	day := func(w int, open, close string) models.BusinessHour {
		return models.BusinessHour{Weekday: w, OpenTime: open, CloseTime: close}
	}
	cases := []struct {
		name  string
		hours []models.BusinessHour
		now   time.Time
		want  NextOpen
		ok    bool
	}{
		{"abre hoy más tarde", []models.BusinessHour{day(4, "08:00", "20:00")}, at(7, 0), NextOpen{0, 4, "08:00"}, true},
		{"cerró hoy: mañana", []models.BusinessHour{day(4, "08:00", "20:00"), day(5, "09:00", "18:00")}, at(21, 0), NextOpen{1, 5, "09:00"}, true},
		{"mañana cerrado: pasado mañana", []models.BusinessHour{day(4, "08:00", "20:00"), {Weekday: 5, Closed: true}, day(6, "10:00", "14:00")}, at(21, 0), NextOpen{2, 6, "10:00"}, true},
		{"solo abre los jueves: la próxima semana", []models.BusinessHour{day(4, "08:00", "20:00")}, at(21, 0), NextOpen{7, 4, "08:00"}, true},
		{"mañana 24 horas", []models.BusinessHour{day(4, "08:00", "20:00"), {Weekday: 5, Is24h: true}}, at(21, 0), NextOpen{1, 5, "00:00"}, true},
		{"sin horario", nil, at(12, 0), NextOpen{}, false},
		{"cerrado todos los días", []models.BusinessHour{{Weekday: 4, Closed: true}, {Weekday: 5, Closed: true}}, at(12, 0), NextOpen{}, false},
		{"fila sin horas se ignora", []models.BusinessHour{{Weekday: 5}, day(6, "10:00", "14:00")}, at(21, 0), NextOpen{2, 6, "10:00"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NextOpening(tc.hours, tc.now)
			if ok != tc.ok || got != tc.want {
				t.Errorf("NextOpening = %+v %v, se esperaba %+v %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
