package agenda

import (
	"testing"
	"time"
)

func TestNeedsReview(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, co)
	in3h := now.Add(3 * time.Hour)
	rules := Intake{Mode: IntakeRules, ReviewNoShows: 2, ReviewNoticeMin: 120}
	cases := []struct {
		name   string
		in     Intake
		h      History
		start  time.Time
		review bool
		why    string
	}{
		{"automático nunca revisa, aunque tenga faltas", Intake{Mode: IntakeAuto, ReviewNoShows: 2}, History{NoShows: 5}, now.Add(10 * time.Minute), false, ""},
		{"manual siempre revisa", Intake{Mode: IntakeManual}, History{Completed: 9}, in3h, true, "manual"},
		{"modo desconocido = manual", Intake{Mode: "otro"}, History{}, in3h, true, "manual"},
		{"reglas: cliente conocido con tiempo se confirma", rules, History{Completed: 3}, in3h, false, ""},
		{"reglas: 2 faltas a revisión", rules, History{Completed: 3, NoShows: 2}, in3h, true, "faltas"},
		{"reglas: 1 falta no basta", rules, History{Completed: 3, NoShows: 1}, in3h, false, ""},
		{"reglas: primera visita con la regla apagada se confirma", rules, History{}, in3h, false, ""},
		{"reglas: primera visita con la regla encendida a revisión", Intake{Mode: IntakeRules, ReviewFirstVisit: true}, History{}, in3h, true, "primera visita"},
		{"reglas: 90 min de anticipación a revisión", rules, History{Completed: 1}, now.Add(90 * time.Minute), true, "poca anticipación"},
		{"reglas: 120 min justos se confirma", rules, History{Completed: 1}, now.Add(120 * time.Minute), false, ""},
		{"reglas todas apagadas: se confirma", Intake{Mode: IntakeRules}, History{NoShows: 9}, now.Add(5 * time.Minute), false, ""},
	}
	for _, c := range cases {
		if got, why := NeedsReview(c.in, c.h, c.start, now); got != c.review || why != c.why {
			t.Errorf("%s: revisión=%v (%q), quería %v (%q)", c.name, got, why, c.review, c.why)
		}
	}
}

func TestInQuiet(t *testing.T) {
	// 18 oct 2026 es domingo.
	at := func(wd time.Weekday, h, m int) time.Time { return time.Date(2026, 10, 18+int(wd), h, m, 0, 0, co) }
	barber := []Hours{{Weekday: 0, Closed: true}, {Weekday: 6, Open: "08:00", Close: "18:00"}}
	for d := 1; d <= 5; d++ {
		barber = append(barber, Hours{Weekday: d, Open: "09:00", Close: "20:00"})
	}
	bar := []Hours{{Weekday: 5, Open: "18:00", Close: "02:00"}} // viernes hasta las 2:00 am
	always := []Hours{}
	for d := 0; d < 7; d++ {
		always = append(always, Hours{Weekday: d, Is24h: true})
	}
	hrs := Quiet{Mode: QuietHours}
	custom := Quiet{Mode: QuietCustom, From: "21:00", To: "07:00"}
	cases := []struct {
		name  string
		q     Quiet
		h     []Hours
		t     time.Time
		quiet bool
	}{
		{"barbería abierta lunes 10:00", hrs, barber, at(time.Monday, 10, 0), false},
		{"barbería lunes 8:59 aún cerrada", hrs, barber, at(time.Monday, 8, 59), true},
		{"barbería lunes 20:00 ya cerró", hrs, barber, at(time.Monday, 20, 0), true},
		{"barbería sábado 8:00 abre temprano", hrs, barber, at(time.Saturday, 8, 0), false},
		{"barbería domingo cerrado", hrs, barber, at(time.Sunday, 12, 0), true},
		{"bar viernes 23:00 abierto", hrs, bar, at(time.Friday, 23, 0), false},
		{"bar sábado 1:30 sigue la franja del viernes", hrs, bar, at(time.Saturday, 1, 30), false},
		{"bar sábado 2:00 ya cerró", hrs, bar, at(time.Saturday, 2, 0), true},
		{"24 horas nunca calla", hrs, always, at(time.Wednesday, 3, 0), false},
		{"sin horario publicado: silencio fijo de noche", hrs, nil, at(time.Monday, 22, 0), true},
		{"sin horario publicado: de día avisa", hrs, nil, at(time.Monday, 12, 0), false},
		{"elegido 21–7, 23:00", custom, barber, at(time.Monday, 23, 0), true},
		{"elegido 21–7, 7:00", custom, barber, at(time.Monday, 7, 0), false},
		{"elegido 13–15 (almuerzo), 14:00", Quiet{Mode: QuietCustom, From: "13:00", To: "15:00"}, barber, at(time.Monday, 14, 0), true},
		{"elegido inválido no calla", Quiet{Mode: QuietCustom, From: "x", To: "07:00"}, barber, at(time.Monday, 23, 0), false},
		{"nunca: avisa aunque esté cerrado", Quiet{Mode: QuietOff}, barber, at(time.Sunday, 3, 0), false},
	}
	for _, c := range cases {
		if got := InQuiet(c.q, c.h, c.t); got != c.quiet {
			t.Errorf("%s: silencio=%v, quería %v", c.name, got, c.quiet)
		}
	}
}
