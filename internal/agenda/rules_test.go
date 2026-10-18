package agenda

import (
	"testing"
	"time"
)

var co = time.FixedZone("America/Bogota", -5*3600)

func at(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, co) }

// Oct 2026: 4 = domingo, 5 = lunes ...
func weekHours() []Hours {
	hs := []Hours{{Weekday: 0, Closed: true}}
	for wd := 1; wd <= 6; wd++ {
		hs = append(hs, Hours{Weekday: wd, Open: "08:00", Close: "18:00"})
	}
	return hs
}

func TestComputeExpiryCases(t *testing.T) {
	hs := weekHours()
	cases := []struct {
		name       string
		now, start time.Time
		want       time.Time
	}{
		{"9pm -> mañana 10am abre 8", at(2026, 10, 5, 21, 0), at(2026, 10, 6, 10, 0), at(2026, 10, 6, 8, 0)},
		{"9pm -> mañana 9am: el piso de apertura gana", at(2026, 10, 5, 21, 0), at(2026, 10, 6, 9, 0), at(2026, 10, 6, 8, 0)},
		{"2pm lun -> en 10 días: tope 24h (mar 2pm abierto)", at(2026, 10, 5, 14, 0), at(2026, 10, 15, 10, 0), at(2026, 10, 6, 14, 0)},
		{"11:50pm -> 12:30am: menos de 2h, 30 min", at(2026, 10, 5, 23, 50), at(2026, 10, 6, 0, 30), at(2026, 10, 6, 0, 20)},
		{"hoy 9am -> hoy 1pm", at(2026, 10, 5, 9, 0), at(2026, 10, 5, 13, 0), at(2026, 10, 5, 11, 0)},
		{"hoy 9am -> hoy 3pm: regla de hoy, no la fórmula", at(2026, 10, 5, 9, 0), at(2026, 10, 5, 15, 0), at(2026, 10, 5, 11, 0)},
		{"en menos de 2h: 30 min", at(2026, 10, 5, 9, 0), at(2026, 10, 5, 10, 30), at(2026, 10, 5, 9, 30)},
		{"sábado 3pm -> en 10 días: tope cae en domingo cerrado", at(2026, 10, 10, 15, 0), at(2026, 10, 20, 10, 0), at(2026, 10, 12, 10, 0)},
		{"11pm -> en 10 días: tope cae de noche", at(2026, 10, 5, 23, 0), at(2026, 10, 20, 10, 0), at(2026, 10, 7, 10, 0)},
	}
	for _, c := range cases {
		got := ComputeExpiry(c.now, c.start, hs)
		if !got.Equal(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
		if got.After(c.start) {
			t.Errorf("%s: expira después de la cita", c.name)
		}
		if got.Before(c.now.Add(MinWindow)) {
			t.Errorf("%s: menos de 15 minutos", c.name)
		}
	}
}

func TestComputeExpiryFloor(t *testing.T) {
	// Sin horario conocido: start-2h == ahora, el piso de 15 minutos manda.
	now, start := at(2026, 10, 5, 23, 0), at(2026, 10, 6, 1, 0)
	if got := ComputeExpiry(now, start, nil); !got.Equal(at(2026, 10, 5, 23, 15)) {
		t.Errorf("piso de 15 min: %v", got)
	}
}

func TestHalfwayPoint(t *testing.T) {
	c := at(2026, 10, 5, 9, 0)
	if got := HalfwayPoint(c, c.Add(2*time.Hour)); !got.Equal(c.Add(time.Hour)) {
		t.Errorf("halfway corto: %v", got)
	}
	if got := HalfwayPoint(c, c.Add(24*time.Hour)); !got.Equal(c.Add(12 * time.Hour)) {
		t.Errorf("halfway largo: %v", got)
	}
}

func TestFreeSlots(t *testing.T) {
	hs := weekHours()
	now := at(2026, 10, 5, 7, 0) // lunes, cerrado todavía
	p := SlotParams{
		Day: at(2026, 10, 5, 0, 0), Now: now, Hours: hs, SlotMinutes: 30, DurationMin: 60,
		LunchStart: "12:00", LunchEnd: "13:00", HorizonDays: 14,
	}
	slots := FreeSlots(p)
	if len(slots) == 0 {
		t.Fatal("sin espacios")
	}
	// Cerrado ahora: no se ofrece nada en la primera media hora (8:00 ni 8:15).
	if slots[0].Before(at(2026, 10, 5, 8, 30)) {
		t.Errorf("primer espacio demasiado temprano: %v", slots[0])
	}
	for _, s := range slots {
		end := s.Add(time.Hour)
		if s.Before(at(2026, 10, 5, 13, 0)) && end.After(at(2026, 10, 5, 12, 0)) {
			t.Errorf("%v cruza el almuerzo", s)
		}
		if end.After(at(2026, 10, 5, 18, 0)) {
			t.Errorf("%v se pasa del cierre", s)
		}
	}
	// Una cita ocupada de 9:00 a 10:00 con margen de 10 min quita lo que choque.
	p.BufferMin = 10
	p.Busy = []Interval{{Start: at(2026, 10, 5, 9, 0), End: at(2026, 10, 5, 10, 10)}}
	for _, s := range FreeSlots(p) {
		if (Interval{Start: s, End: s.Add(70 * time.Minute)}).Overlaps(p.Busy[0]) {
			t.Errorf("%v choca con la cita existente", s)
		}
	}
	// Domingo cerrado: nada.
	p.Day = at(2026, 10, 11, 0, 0)
	if got := FreeSlots(p); len(got) != 0 {
		t.Errorf("domingo debería estar vacío, hay %d", len(got))
	}
}

func TestFreeSlotsMinNoticeAndOpenNow(t *testing.T) {
	hs := weekHours()
	now := at(2026, 10, 5, 10, 0) // abierto
	p := SlotParams{Day: at(2026, 10, 5, 0, 0), Now: now, Hours: hs, SlotMinutes: 30, DurationMin: 30, HorizonDays: 14, MinNoticeMin: 10}
	slots := FreeSlots(p)
	// Anticipación mínima efectiva 45 min: la primera es 11:00 (10:45 no existe en la rejilla).
	if !slots[0].Equal(at(2026, 10, 5, 11, 0)) {
		t.Errorf("primer espacio: %v", slots[0])
	}
	// Abierto: mañana a las 8:00 sí se puede pedir.
	p.Day = at(2026, 10, 6, 0, 0)
	if got := FreeSlots(p); !got[0].Equal(at(2026, 10, 6, 8, 0)) {
		t.Errorf("mañana abre 8:00: %v", got[0])
	}
}

func TestServiceDurationOnGrid(t *testing.T) {
	hs := weekHours()
	p := SlotParams{Day: at(2026, 10, 6, 0, 0), Now: at(2026, 10, 5, 9, 0), Hours: hs, SlotMinutes: 30, DurationMin: 20, HorizonDays: 14}
	slots := FreeSlots(p)
	if !slots[len(slots)-1].Equal(at(2026, 10, 6, 17, 30)) {
		t.Errorf("último servicio de 20 min: %v", slots[len(slots)-1])
	}
}

func TestOvernightWindow(t *testing.T) {
	hs := []Hours{{Weekday: 5, Open: "18:00", Close: "02:00"}}
	fri := at(2026, 10, 9, 0, 0)
	if !IsOpenAt(hs, at(2026, 10, 10, 1, 0)) {
		t.Error("sábado 1am sigue abierto por el horario del viernes")
	}
	if IsOpenAt(hs, at(2026, 10, 10, 3, 0)) {
		t.Error("sábado 3am cerrado")
	}
	if _, c, ok := WindowOn(hs, fri); !ok || !c.Equal(at(2026, 10, 10, 2, 0)) {
		t.Errorf("cierre nocturno: %v", c)
	}
}

func TestOutcome(t *testing.T) {
	y, n := true, false
	cases := []struct {
		c, b  *bool
		final bool
		want  string
	}{
		{&y, &y, false, "completed"},
		{&n, &n, false, "no_show"},
		{&y, &n, false, "disputed"},
		{&n, &y, false, "disputed"},
		{&y, nil, false, ""},
		{&y, nil, true, "completed"},
		{nil, &n, true, "no_show"},
		{nil, nil, false, ""},
		{nil, nil, true, "completed"},
	}
	for i, c := range cases {
		if got := Outcome(c.c, c.b, c.final); got != c.want {
			t.Errorf("caso %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestInSilence(t *testing.T) {
	if !InSilence(at(2026, 10, 5, 22, 0)) || !InSilence(at(2026, 10, 5, 6, 59)) || InSilence(at(2026, 10, 5, 7, 0)) || InSilence(at(2026, 10, 5, 20, 59)) {
		t.Error("horas de silencio mal")
	}
}
