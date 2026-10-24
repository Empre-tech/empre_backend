package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// 3.48: pasarela, mosaico, rutas y vecinos.

// barrioCand: una oferta de un negocio con su tipo (categoría), su punto y su ahorro (sp/rf). Mismo `ent` = mismo negocio.
func barrioCand(title string, ent, cat uuid.UUID, lat, long float64, sp, rf int, endsIn, age time.Duration, now time.Time) MarketCandidate {
	s, r := sp, rf
	return MarketCandidate{Offer: models.Offer{
		ID: uuid.New(), Title: title, EntityID: ent, SpecialPrice: &s, ReferencePrice: &r,
		StartsAt: now.Add(-age), CreatedAt: now.Add(-age), EndsAt: now.Add(endsIn),
		Entity: models.Entity{ID: ent, CategoryID: cat, Latitude: lat, Longitude: long},
	}}
}

func TestPickSpotlight(t *testing.T) {
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, marketTZ)
	food, beauty, sport := uuid.New(), uuid.New(), uuid.New()
	today := barrioCand("Mitad hoy", uuid.New(), food, 0, 0, 5000, 10000, 8*time.Hour, 48*time.Hour, now)
	// Termina antes, pero es del mismo tipo que la de hoy: la pasarela busca otro tipo.
	sameType := barrioCand("Comida pronto", uuid.New(), food, 0, 0, 1000, 1500, 30*time.Minute, 48*time.Hour, now)
	soon := barrioCand("Corte pronto", uuid.New(), beauty, 0, 0, 1500, 2000, 45*time.Minute, 48*time.Hour, now)
	fresh := barrioCand("Nueva", uuid.New(), sport, 0, 0, 1000, 3000, 72*time.Hour, 2*time.Hour, now)
	old := barrioCand("Vieja", uuid.New(), sport, 0, 0, 1000, 3000, 72*time.Hour, 10*24*time.Hour, now)
	got := PickSpotlight([]MarketCandidate{today, sameType, soon, fresh, old}, now)
	want := []string{SpotlightSaveToday + ":Mitad hoy", SpotlightEndsFirst + ":Corte pronto", SpotlightNewWeek + ":Nueva"}
	if len(got) != len(want) {
		t.Fatalf("diapositivas: %d", len(got))
	}
	for i, g := range got {
		if g.Rule+":"+g.Cand.Offer.Title != want[i] {
			t.Errorf("%d: %s:%s, quería %s", i, g.Rule, g.Cand.Offer.Title, want[i])
		}
	}
	// Una regla sin candidata no sale (aquí no hay nada nuevo ni nada que termine hoy).
	if got := PickSpotlight([]MarketCandidate{old}, now); len(got) != 1 || got[0].Rule != SpotlightEndsFirst {
		t.Fatalf("solo «termina antes»: %v", got)
	}
}

func TestPickBento(t *testing.T) {
	now := time.Now()
	food, beauty, shop := uuid.New(), uuid.New(), uuid.New()
	ana := uuid.New()
	a := barrioCand("Café", ana, food, 0, 0, 9000, 14000, time.Hour, time.Hour, now)             // ahorra 5.000
	b := barrioCand("Almuerzo", uuid.New(), food, 0, 0, 15000, 21000, time.Hour, time.Hour, now) // 6.000, mismo tipo
	c := barrioCand("Corte", uuid.New(), beauty, 0, 0, 28000, 35000, time.Hour, time.Hour, now)  // 7.000
	d := barrioCand("Otro de Ana", ana, shop, 0, 0, 1000, 30000, time.Hour, time.Hour, now)      // mismo negocio que a
	e := barrioCand("Sin ahorro", uuid.New(), shop, 0, 0, 1000, 1000, time.Hour, time.Hour, now)
	got := PickBento([]MarketCandidate{a, b, c, d, e}, map[uuid.UUID]bool{}, BentoMax, false)
	titles := ""
	for _, g := range got {
		titles += g.Offer.Title + ","
	}
	// Por porcentaje: d (97 %) ocupa a Ana; b (29 %, el mejor de comida); c (20 %). a queda fuera (mismo tipo que b y
	// mismo negocio que d).
	if titles != "Otro de Ana,Almuerzo,Corte," {
		t.Fatalf("mosaico: %s", titles)
	}
	// Sin el corte (ya está en la pasarela) queda un solo tipo, comida, y gana el café (36 % contra 29 %).
	if got := PickBento([]MarketCandidate{a, b, c}, map[uuid.UUID]bool{c.Offer.ID: true}, BentoMax, false); len(got) != 1 || got[0].Offer.ID != a.Offer.ID {
		t.Fatalf("sin las de la pasarela: %d piezas", len(got))
	}
}

func TestPickBentoBarrioFirst(t *testing.T) {
	now := time.Now()
	far := barrioCand("Hotel lejos", uuid.New(), uuid.New(), 0, 0, 1000, 10000, time.Hour, time.Hour, now) // 90 %
	near := barrioCand("Café aquí", uuid.New(), uuid.New(), 0, 0, 9000, 14000, time.Hour, time.Hour, now)  // 36 %
	far.DistanceM, near.DistanceM = 4400, 250
	if got := PickBento([]MarketCandidate{far, near}, map[uuid.UUID]bool{}, BentoMax, true); got[0].Offer.ID != near.Offer.ID {
		t.Fatalf("con ubicación, primero lo del barrio: %s", got[0].Offer.Title)
	}
	if got := PickBento([]MarketCandidate{far, near}, map[uuid.UUID]bool{}, BentoMax, false); got[0].Offer.ID != far.Offer.ID {
		t.Fatalf("sin ubicación, por ahorro: %s", got[0].Offer.Title)
	}
}

func TestBuildRoutes(t *testing.T) {
	now := time.Now()
	food, beauty, shop, sport := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	// Centro: tres tipos a menos de 1 km. (0,001° de latitud ≈ 111 m.)
	nails := barrioCand("Uñas", uuid.New(), beauty, 11.2400, -74.2100, 35000, 50000, time.Hour, time.Hour, now)
	coffee := barrioCand("Café", uuid.New(), food, 11.2430, -74.2100, 9000, 14000, time.Hour, time.Hour, now)
	hat := barrioCand("Sombrero", uuid.New(), shop, 11.2410, -74.2100, 60000, 80000, time.Hour, time.Hour, now)
	barber := barrioCand("Corte", uuid.New(), beauty, 11.2405, -74.2100, 28000, 35000, time.Hour, time.Hour, now) // mismo tipo que uñas
	// Lejos (≈ 4,4 km): no entra en la ruta del Centro.
	surf := barrioCand("Surf", uuid.New(), sport, 11.2000, -74.2100, 60000, 90000, time.Hour, time.Hour, now)
	routes := BuildRoutes([]MarketCandidate{nails, coffee, hat, barber, surf}, RoutesMax, false)
	if len(routes) != 1 {
		t.Fatalf("rutas: %d", len(routes))
	}
	r := routes[0]
	// Parte del mayor ahorro (surf queda solo); la ruta del Centro empieza por el sombrero (20.000) y sigue por cercanía.
	order := ""
	for _, s := range r.Stops {
		order += s.Offer.Title + ","
	}
	if order != "Sombrero,Uñas,Café," {
		t.Fatalf("paradas: %s", order)
	}
	if r.Saving != 20000+15000+5000 {
		t.Fatalf("ahorro: %d", r.Saving)
	}
	if r.WalkM < 300 || r.WalkM > 500 {
		t.Fatalf("recorrido: %.0f m", r.WalkM)
	}
}

func TestBuildNeighbors(t *testing.T) {
	now := time.Now()
	cat := uuid.New()
	ana, juan := uuid.New(), uuid.New()
	a1 := barrioCand("Café", ana, cat, 0, 0, 9000, 14000, time.Hour, time.Hour, now)
	a2 := barrioCand("Pan", ana, cat, 0, 0, 1000, 9000, 3*time.Hour, time.Hour, now) // más ahorro: es la de Ana
	j := barrioCand("Corte", juan, cat, 0, 0, 1000, 2000, 30*time.Minute, 90*time.Minute, now)
	a1.DistanceM, a2.DistanceM, j.DistanceM = 300, 300, 100
	got := BuildNeighbors([]MarketCandidate{a1, a2, j}, true, now, NeighborsMax)
	if len(got) != 2 || got[0].Cand.Offer.Title != "Corte" || got[1].Cand.Offer.Title != "Pan" {
		t.Fatalf("vecinos: %v", got)
	}
	// Al corte le quedan 30 de 120 min: un cuarto del anillo.
	if l := got[0].TimeLeft; l < 0.24 || l > 0.26 {
		t.Fatalf("anillo: %.2f", l)
	}
}
