package models

import (
	"testing"
	"time"
)

// La disponibilidad («2 cupos disponibles») deja de valer a las 6 horas sin
// actualizar; entonces solo queda el abierto/cerrado por horario.
func TestBusinessStatus_CurrentHonorsTheSixHourTTL(t *testing.T) {
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		updated time.Time
		text    string
		want    string
	}{
		{"recién actualizada", now.Add(-5 * time.Minute), "2 cupos disponibles", "2 cupos disponibles"},
		{"a 5 h 59 min sigue vigente", now.Add(-(6*time.Hour - time.Minute)), "3 mesas disponibles", "3 mesas disponibles"},
		{"justo a las 6 h todavía vale", now.Add(-6 * time.Hour), "Producto disponible", "Producto disponible"},
		{"pasadas las 6 h se oculta", now.Add(-(6*time.Hour + time.Second)), "2 cupos disponibles", ""},
		{"de hace un día se oculta", now.Add(-24 * time.Hour), "2 cupos disponibles", ""},
		{"sin texto no hay nada que mostrar", now, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := BusinessStatus{Availability: tc.text, UpdatedAt: tc.updated}
			if got := status.Current(now); got != tc.want {
				t.Errorf("Current = %q, se esperaba %q", got, tc.want)
			}
		})
	}
}
