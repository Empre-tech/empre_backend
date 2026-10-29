package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// 3.49: rutas con tema, nivel y hora.

// planCand: una oferta de un negocio con su categoría (por nombre), su punto, su zona y su horario de todos los días
// (open/close «HH:MM»; vacío = sin horario).
func planCand(title, cat string, lat, long float64, zone *uuid.UUID, open, close string, sp, rf int, now time.Time) MarketCandidate {
	c := barrioCand(title, uuid.New(), uuid.New(), lat, long, sp, rf, 24*time.Hour, time.Hour, now)
	c.Offer.Entity.Category = models.Category{Name: cat}
	c.Offer.Entity.ZoneID = zone
	if open != "" {
		for d := 0; d < 7; d++ {
			c.Offer.Entity.Hours = append(c.Offer.Entity.Hours, models.BusinessHour{Weekday: d, OpenTime: open, CloseTime: close})
		}
	}
	return c
}

func stopsOf(r PlanRoute) string {
	s := ""
	for _, st := range r.Stops {
		s += st.Cand.Offer.Title
		if st.At != nil {
			s += "@" + st.At.In(marketTZ).Format("15:04")
		}
		s += ","
	}
	return s
}

func TestBuildPlansWalkThenZoneThenDay(t *testing.T) {
	now := time.Date(2026, 10, 24, 14, 10, 0, 0, marketTZ) // sábado, 2:10 pm
	centro := uuid.New()
	// A pie (≈ 100–300 m): uñas, café y artesanías, todos abiertos en la tarde.
	nails := planCand("Uñas", "Belleza y peluquería", 11.2400, -74.2100, &centro, "09:00", "19:00", 35000, 50000, now)
	coffee := planCand("Café", "Cafeterías y postres", 11.2410, -74.2100, &centro, "07:00", "20:00", 9000, 14000, now)
	hat := planCand("Sombrero", "Tiendas y comercio", 11.2420, -74.2100, &centro, "09:00", "18:00", 60000, 80000, now)
	got := BuildPlans([]MarketCandidate{nails, coffee, hat}, PlanOptions{HasLocation: true, Now: now})
	if len(got) != 1 || got[0].Theme != ThemePamper || got[0].Level != PlanLevelWalk {
		t.Fatalf("tarde de consentirse a pie: %+v", got)
	}
	// Empieza en la siguiente media hora (2:30) y sigue a las 3:30 y 4:00.
	if s := stopsOf(got[0]); s != "Uñas@14:30,Café@15:30,Sombrero@16:00," {
		t.Fatalf("paradas: %s", s)
	}
	if got[0].Saving != 15000+5000+20000 {
		t.Fatalf("ahorro: %d", got[0].Saving)
	}

	// Separados ≈ 2 km, pero en la misma zona: «en tu barrio».
	coffeeFar := planCand("Café", "Cafeterías y postres", 11.2580, -74.2100, &centro, "07:00", "20:00", 9000, 14000, now)
	got = BuildPlans([]MarketCandidate{nails, coffeeFar}, PlanOptions{HasLocation: true, Now: now})
	if len(got) != 1 || got[0].Level != PlanLevelZone {
		t.Fatalf("en tu barrio: %+v", got)
	}

	// Sin ubicación (o nada cerca): «tu día», por momentos, cada uno abierto a su hora.
	morning := time.Date(2026, 10, 24, 7, 0, 0, 0, marketTZ)
	gym := planCand("Gimnasio", "Deporte y recreación", 11.2000, -74.2300, nil, "05:00", "21:00", 30000, 60000, morning)
	spa := planCand("Spa", "Salud y bienestar", 11.2470, -74.1960, nil, "09:00", "19:00", 20000, 45000, morning) // mismo grupo que el gimnasio
	store := planCand("Tienda", "Tiendas y comercio", 11.2300, -74.2100, nil, "10:00", "21:00", 40000, 60000, morning)
	bar := planCand("Bar", "Bares y vida nocturna", 11.2650, -74.1900, nil, "17:00", "02:00", 12000, 24000, morning)
	got = BuildPlans([]MarketCandidate{gym, spa, store, bar}, PlanOptions{HasLocation: false, Now: morning})
	if len(got) != 1 || got[0].Theme != ThemeDay || got[0].Level != PlanLevelDay {
		t.Fatalf("tu día: %+v", got)
	}
	// A las 9 el gimnasio (la tienda, con más ahorro, abre a las 10); a las 15 la tienda; a las 19 el bar. El spa no
	// entra: es del mismo grupo de Explorar que el gimnasio («Belleza y salud»).
	if s := stopsOf(got[0]); s != "Gimnasio@09:00,Tienda@15:00,Bar@19:00," {
		t.Fatalf("momentos: %s", s)
	}
}

func TestBuildDayTomorrowAtNight(t *testing.T) {
	// 8 pm: hoy ya no quedan dos momentos, así que el plan es para mañana (9, 15 y 19 h).
	night := time.Date(2026, 10, 24, 20, 0, 0, 0, marketTZ)
	gym := planCand("Gimnasio", "Deporte y recreación", 11.2000, -74.2300, nil, "05:00", "21:00", 30000, 60000, night)
	store := planCand("Tienda", "Tiendas y comercio", 11.2300, -74.2100, nil, "10:00", "21:00", 20000, 40000, night)
	bar := planCand("Bar", "Bares y vida nocturna", 11.2650, -74.1900, nil, "17:00", "02:00", 12000, 24000, night)
	got := BuildPlans([]MarketCandidate{gym, store, bar}, PlanOptions{HasLocation: false, Now: night})
	if len(got) != 1 || got[0].Stops[0].At == nil || got[0].Stops[0].At.In(marketTZ).Day() != 25 {
		t.Fatalf("mañana: %+v", got)
	}
	if s := stopsOf(got[0]); s != "Gimnasio@09:00,Tienda@15:00,Bar@19:00," {
		t.Fatalf("momentos de mañana: %s", s)
	}
}

func TestBuildPlansRespectsHoursAndThemes(t *testing.T) {
	now := time.Date(2026, 10, 24, 14, 10, 0, 0, marketTZ)
	z := uuid.New()
	nails := planCand("Uñas", "Belleza y peluquería", 11.2400, -74.2100, &z, "09:00", "19:00", 35000, 50000, now)
	// El café cierra a las 3 pm: a las 3:30 ya no sirve; entra el restaurante (también vale como segunda parada).
	closedCoffee := planCand("Café que cierra", "Cafeterías y postres", 11.2410, -74.2100, &z, "07:00", "15:00", 9000, 20000, now)
	lunch := planCand("Almuerzo", "Restaurantes", 11.2415, -74.2100, &z, "11:00", "22:00", 14000, 20000, now)
	got := BuildPlans([]MarketCandidate{nails, closedCoffee, lunch}, PlanOptions{HasLocation: true, Now: now})
	if len(got) != 1 || stopsOf(got[0]) != "Uñas@14:30,Almuerzo@15:30," {
		t.Fatalf("el cerrado no entra: %+v", stopsOf(got[0]))
	}

	// Sin horario: solo a pie y sin hora. Lejos y sin horario: no hay ruta con tema; queda la mezcla de siempre.
	plainA := planCand("Uñas sin horario", "Belleza y peluquería", 11.2400, -74.2100, &z, "", "", 35000, 50000, now)
	plainB := planCand("Café sin horario", "Cafeterías y postres", 11.2410, -74.2100, &z, "", "", 9000, 14000, now)
	got = BuildPlans([]MarketCandidate{plainA, plainB}, PlanOptions{HasLocation: true, Now: now})
	if len(got) != 1 || got[0].Level != PlanLevelWalk || stopsOf(got[0]) != "Uñas sin horario,Café sin horario," {
		t.Fatalf("sin horario, a pie y sin hora: %+v", got)
	}

	// El respaldo («Ruta por {zona}») tampoco propone lo que está cerrado ahora.
	late := time.Date(2026, 10, 24, 19, 5, 0, 0, marketTZ)
	ev := planCand("Eventos", "Eventos y fiestas", 11.2400, -74.2100, &z, "09:00", "18:00", 1000, 60000, late) // cerrado
	sch := planCand("Colegio", "Educación", 11.2405, -74.2100, &z, "", "", 25000, 50000, late)
	shop := planCand("Almacén", "Tiendas y comercio", 11.2410, -74.2100, &z, "", "", 45000, 60000, late)
	pet := planCand("Veterinaria", "Mascotas", 11.2415, -74.2100, &z, "", "", 35000, 50000, late)
	for _, r := range BuildPlans([]MarketCandidate{ev, sch, shop, pet}, PlanOptions{HasLocation: true, Now: late}) {
		for _, st := range r.Stops {
			if st.Cand.Offer.Title == "Eventos" {
				t.Fatalf("propuso un negocio cerrado: %s", stopsOf(r))
			}
		}
	}

	// El respaldo busca un orden en que cada parada esté abierta a su turno: el gimnasio cierra a las 9 pm, así que
	// va primero (a las 7:30), aunque así se camine un poco más. Es el caso del emulador del 24 oct.
	hotel := planCand("Hotel", "Turismo y hospedaje", 11.2080, -74.2270, &z, "00:00", "23:59", 210000, 280000, late)
	gym := planCand("Gimnasio", "Deporte y recreación", 11.2050, -74.2250, &z, "05:00", "21:00", 30000, 60000, late)
	club := planCand("Club", "Bares y vida nocturna", 11.2030, -74.2290, &z, "17:00", "02:00", 22000, 44000, late)
	got = BuildPlans([]MarketCandidate{hotel, gym, club}, PlanOptions{HasLocation: true, Now: late})
	if len(got) != 1 || got[0].Theme == ThemeDay {
		t.Fatalf("respaldo con horas: %+v", got)
	}
	for _, st := range got[0].Stops {
		if st.At == nil || !openAt(st.Cand, *st.At) {
			t.Fatalf("cada parada abierta a su hora: %s", stopsOf(got[0]))
		}
	}

	// Fuera de su ventana un tema no se arma: a las 9 pm no hay «Tarde de consentirse».
	night := time.Date(2026, 10, 24, 21, 0, 0, 0, marketTZ)
	n := planCand("Uñas", "Belleza y peluquería", 11.2400, -74.2100, &z, "00:00", "23:59", 35000, 50000, night)
	c := planCand("Café", "Cafeterías y postres", 11.2410, -74.2100, &z, "00:00", "23:59", 9000, 14000, night)
	for _, r := range BuildPlans([]MarketCandidate{n, c}, PlanOptions{HasLocation: true, Now: night}) {
		if r.Theme == ThemePamper {
			t.Fatalf("de noche no hay tarde de consentirse")
		}
	}
}

func TestNextStops(t *testing.T) {
	now := time.Date(2026, 10, 24, 14, 10, 0, 0, marketTZ)
	z := uuid.New()
	coffee := planCand("Café", "Cafeterías y postres", 11.2410, -74.2100, &z, "07:00", "20:00", 9000, 14000, now)
	cake := planCand("Pastel", "Cafeterías y postres", 11.2411, -74.2100, &z, "07:00", "20:00", 1000, 9000, now) // mismo tipo
	nails := planCand("Uñas", "Belleza y peluquería", 11.2400, -74.2100, &z, "09:00", "19:00", 35000, 50000, now)
	hat := planCand("Sombrero", "Tiendas y comercio", 11.2430, -74.2100, &z, "09:00", "18:00", 60000, 80000, now)
	closed := planCand("Cerrado", "Mascotas", 11.2409, -74.2100, &z, "06:00", "08:00", 1000, 5000, now)
	got := NextStops(coffee, []MarketCandidate{coffee, cake, nails, hat, closed}, now, 2)
	if len(got) != 2 || got[0].Offer.Title != "Uñas" || got[1].Offer.Title != "Sombrero" {
		t.Fatalf("completa tu plan: %v", got)
	}
}

// serviceCand: un servicio del catálogo de un negocio sin oferta (3.50), como lo arma reviewedServices.
func serviceCand(name, cat string, lat, long float64, zone *uuid.UUID, open, close string, price, dur int, rating float64, now time.Time) MarketCandidate {
	c := planCand(name, cat, lat, long, zone, open, close, 0, 0, now)
	c.Offer.SpecialPrice, c.Offer.ReferencePrice = nil, nil
	p, d := price, dur
	item := models.ServiceItem{ID: c.Offer.ID, Name: name, Price: &p, DurationMin: &d, Bookable: true}
	c.Service, c.Bookable, c.Rating = &item, true, rating
	return c
}

func TestPlansWithServices(t *testing.T) {
	now := time.Date(2026, 10, 24, 14, 40, 0, 0, marketTZ) // 2:40 pm → la ruta empieza a las 3:00
	z := uuid.New()
	nails := planCand("Uñas", "Belleza y peluquería", 11.2400, -74.2100, &z, "09:00", "19:00", 35000, 50000, now)
	hat := planCand("Sombrero", "Tiendas y comercio", 11.2427, -74.2100, &z, "09:00", "18:00", 60000, 80000, now)
	// No hay oferta de café: entra el servicio de Café Ribera (★ 4,9), de 30 min.
	coffee := serviceCand("Dos cafés de origen", "Cafeterías y postres", 11.2418, -74.2100, &z, "07:00", "20:00", 9000, 30, 4.9, now)
	got := BuildPlans([]MarketCandidate{nails, hat}, PlanOptions{HasLocation: true, Now: now, Services: []MarketCandidate{coffee}})
	if len(got) != 1 || got[0].Theme != ThemePamper {
		t.Fatalf("tarde de consentirse con servicio: %+v", got)
	}
	r := got[0]
	if s := stopsOf(r); s != "Uñas@15:00,Dos cafés de origen@16:00,Sombrero@16:35," {
		// 3:00 uñas; +1 h (sin duración conocida) → 4:00 café; café dura 30 min + 5 min a pie → 4:35 sombrero.
		t.Fatalf("paradas y horas: %s", s)
	}
	if r.Stops[1].Cand.Service == nil || r.Saving != 15000+20000 {
		t.Fatalf("el servicio no suma ahorro: %d", r.Saving)
	}
	total := 0
	for _, st := range r.Stops {
		total += StopPrice(st.Cand)
	}
	if total != 35000+9000+60000 {
		t.Fatalf("lo que cuesta el plan: %d", total)
	}

	// Un servicio nunca es la primera parada: sin ofertas no hay ruta con tema.
	got = BuildPlans(nil, PlanOptions{HasLocation: true, Now: now, Services: []MarketCandidate{coffee}})
	for _, r := range got {
		if r.Stops[0].Cand.Service != nil {
			t.Fatalf("la primera parada es un servicio: %s", stopsOf(r))
		}
	}

	// Si hay una oferta que llena la parada, gana a cualquier servicio.
	coffeeOffer := planCand("Café en oferta", "Cafeterías y postres", 11.2418, -74.2100, &z, "07:00", "20:00", 8000, 10000, now)
	got = BuildPlans([]MarketCandidate{nails, coffeeOffer, hat}, PlanOptions{HasLocation: true, Now: now, Services: []MarketCandidate{coffee}})
	if len(got) != 1 || got[0].Stops[1].Cand.Service != nil {
		t.Fatalf("la oferta gana al servicio: %s", stopsOf(got[0]))
	}
}

func TestBetterFirstPrefersRatingWhenSimilar(t *testing.T) {
	now := time.Now()
	a := planCand("Buena", "Restaurantes", 0, 0, nil, "", "", 10000, 20000, now)          // ahorra 10.000
	b := planCand("Mejor valorada", "Restaurantes", 0, 0, nil, "", "", 10500, 20000, now) // 9.500: misma franja
	c := planCand("Mucho ahorro", "Restaurantes", 0, 0, nil, "", "", 1000, 40000, now)    // 39.000: otra franja
	a.Rating, b.Rating, c.Rating = 4.1, 4.9, 3.5
	list := []MarketCandidate{a, b, c}
	betterFirst(list, false)
	if list[0].Offer.Title != "Mucho ahorro" || list[1].Offer.Title != "Mejor valorada" {
		t.Fatalf("orden: %s, %s, %s", list[0].Offer.Title, list[1].Offer.Title, list[2].Offer.Title)
	}
}
