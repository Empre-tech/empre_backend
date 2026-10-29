package services

import (
	"math"
	"sort"
	"time"

	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// Rutas del barrio con tema, nivel y hora (3.49, mockup rutas_del_barrio.html). Parte pura: con las candidatas ya de
// la ciudad arma hasta 3 planes, uno por tema. Cada tema dice qué tipos de negocio lleva y a qué horas tiene sentido;
// para cada uno se prueba primero «a pie» (todo a menos de 1 km), después «en tu barrio» (la misma zona, hasta 3 km) y,
// si ningún tema arma nada, «tu día en la ciudad»: tres momentos (mañana, tarde, noche) ordenados por hora. Cada parada
// está abierta a la hora que propone el plan y su oferta sigue vigente hasta esa hora. Nada pagado, sin conteos.

const (
	PlanLevelWalk = "walk" // a pie
	PlanLevelZone = "zone" // en tu barrio
	PlanLevelDay  = "day"  // tu día en la ciudad

	PlanZoneRadiusM = 3000
	planAnchors     = 8 // anclas por tema (las de más ahorro): suficiente sin probar todas las combinaciones
	planFirstGap    = 60 * time.Minute
	planNextGap     = 30 * time.Minute
)

// Temas: los nombres los pone la app (por la clave); aquí solo qué lleva cada uno y cuándo.
const (
	ThemePamper  = "pamper"  // Tarde de consentirse
	ThemeErrands = "errands" // Día de vueltas
	ThemeActive  = "active"  // Mañana activa
	ThemeNight   = "night"   // Noche en el barrio
	ThemeSweet   = "sweet"   // Antojo dulce
	ThemePet     = "pet"     // Plan con tu mascota
	ThemeZone    = "zone"    // Ruta por {zona}: cualquier mezcla
	ThemeDay     = "day"     // Tu {día} en {ciudad}
)

// planSlot: las categorías (por nombre, sin tildes) que llenan una parada. Vacío = cualquiera de otro tipo.
type planSlot []string

type planTheme struct {
	Key        string
	Slots      []planSlot // las obligatorias primero; Optional = la última se puede dejar sin llenar
	Optional   bool
	From, To   int  // horas del día en que aplica (To 24 = hasta medianoche)
	SameKindOK bool // «Antojo dulce» repite «Comer y beber» a propósito
}

var planThemes = []planTheme{
	{Key: ThemePamper, Slots: []planSlot{{"belleza y peluqueria", "salud y bienestar"}, {"cafeterias y postres", "restaurantes"}, {}}, Optional: true, From: 13, To: 19},
	{Key: ThemeErrands, Slots: []planSlot{{"servicios del hogar", "automotriz", "tecnologia"}, {"restaurantes"}, {"tiendas y comercio", "mascotas", "otros"}}, Optional: true, From: 8, To: 18},
	{Key: ThemeActive, Slots: []planSlot{{"deporte y recreacion"}, {"cafeterias y postres", "restaurantes"}, {}}, Optional: true, From: 5, To: 11},
	{Key: ThemeNight, Slots: []planSlot{{"restaurantes"}, {"bares y vida nocturna", "eventos y fiestas"}, {}}, Optional: true, From: 17, To: 24},
	{Key: ThemeSweet, Slots: []planSlot{{"cafeterias y postres"}, {"cafeterias y postres"}, {"cafeterias y postres"}}, From: 0, To: 24, SameKindOK: true},
	{Key: ThemePet, Slots: []planSlot{{"mascotas"}, {"cafeterias y postres", "restaurantes", "turismo y hospedaje"}}, From: 0, To: 24},
}

// Momentos de «tu día»: la hora sugerida de cada uno.
var dayMoments = []int{9, 15, 19}

// PlanStop es una parada: la oferta y la hora sugerida (nil si el negocio no tiene horario).
type PlanStop struct {
	Cand MarketCandidate
	At   *time.Time
}

// PlanRoute es una ruta del barrio con su tema y su nivel.
type PlanRoute struct {
	Theme  string
	Level  string
	ZoneID *uuid.UUID // la de la primera parada (para «Ruta por {zona}»)
	Stops  []PlanStop
	Saving int
	WalkM  float64
}

type PlanOptions struct {
	HasLocation bool
	Now         time.Time
	Max         int
	// Services: servicios de negocios sin oferta con buenas reseñas (3.50). Llenan una parada que ninguna oferta
	// llena; nunca la primera, así toda ruta lleva al menos una oferta.
	Services []MarketCandidate
}

// walkGap: lo que se tarda caminando de un negocio al otro (80 m por minuto), redondeado a 5 min.
func walkGap(a, b MarketCandidate) time.Duration {
	min := int(entityDistance(a, b)/80) + 1
	return time.Duration((min+4)/5*5) * time.Minute
}

// nextAt: la hora de la parada siguiente. Si la anterior tiene duración conocida (un servicio con agenda), es esa
// duración más el tramo a pie; si no, los saltos fijos de siempre (1 h tras la primera, 30 min después).
func nextAt(prev MarketCandidate, prevAt time.Time, first bool, b MarketCandidate) time.Time {
	if prev.Service != nil && prev.Service.DurationMin != nil {
		return prevAt.Add(time.Duration(*prev.Service.DurationMin)*time.Minute + walkGap(prev, b))
	}
	if first {
		return prevAt.Add(planFirstGap)
	}
	return prevAt.Add(planNextGap)
}

// betterFirst ordena por ahorro y, entre dos parecidas (de la misma franja de ahorro), por valoración (3.50).
func betterFirst(list []MarketCandidate, hasLoc bool) {
	band := func(c MarketCandidate) int {
		s := SavingPesos(c)
		if s <= 0 {
			return 0
		}
		return int(math.Log(float64(s)) / math.Log(1.25)) // franjas del 25 %
	}
	sort.SliceStable(list, func(i, j int) bool {
		if bi, bj := barrioFirst(list[i], hasLoc), barrioFirst(list[j], hasLoc); bi != bj {
			return bi
		}
		if bi, bj := band(list[i]), band(list[j]); bi != bj {
			return bi > bj
		}
		if list[i].Rating != list[j].Rating {
			return list[i].Rating > list[j].Rating
		}
		return SavingPesos(list[i]) > SavingPesos(list[j])
	})
}

func slotOK(s planSlot, c MarketCandidate) bool {
	if len(s) == 0 {
		return true
	}
	n := FoldText(c.Offer.Entity.Category.Name)
	for _, want := range s {
		if n == want {
			return true
		}
	}
	return false
}

// openAt: abierto a esa hora (según su horario) y con la oferta vigente hasta entonces.
func openAt(c MarketCandidate, at time.Time) bool {
	return c.Offer.EndsAt.After(at) && len(c.Offer.Entity.Hours) > 0 && utils.IsOpenNow(c.Offer.Entity.Hours, at.In(marketTZ))
}

// stopAt decide si una oferta puede ser parada a esa hora y con qué hora sugerida. Con horario: solo si está abierta
// (y la hora se muestra). Sin horario: solo a pie, sin hora, y con la oferta vigente ahora.
func stopAt(c MarketCandidate, level string, at time.Time) (ok bool, when *time.Time) {
	if len(c.Offer.Entity.Hours) > 0 {
		if !openAt(c, at) {
			return false, nil
		}
		t := at
		return true, &t
	}
	return level == PlanLevelWalk && c.Offer.EndsAt.After(at), nil
}

// planStart: la primera hora del plan, en la siguiente media hora (y dentro de la ventana del tema).
func planStart(now time.Time, from int) time.Time {
	t := now.In(marketTZ).Truncate(30 * time.Minute).Add(30 * time.Minute)
	if t.Hour() < from {
		y, m, d := t.Date()
		t = time.Date(y, m, d, from, 0, 0, 0, marketTZ)
	}
	return t
}

// themeApplies: la ventana del tema todavía cabe hoy (el plan empieza dentro de ella).
func themeApplies(th planTheme, start time.Time) bool {
	h := start.In(marketTZ).Hour()
	return h >= th.From && h < th.To
}

// near: la regla de distancia del nivel entre dos paradas (siempre contra la primera, para que la ruta no se estire).
func nearFor(level string, a, b MarketCandidate) bool {
	switch level {
	case PlanLevelWalk:
		return entityDistance(a, b) <= RouteRadiusM
	case PlanLevelZone:
		za, zb := a.Offer.Entity.ZoneID, b.Offer.Entity.ZoneID
		return za != nil && zb != nil && *za == *zb && entityDistance(a, b) <= PlanZoneRadiusM
	}
	return true
}

// buildThemed intenta un tema en un nivel: ancla con el mayor ahorro que llena la primera parada y, para las demás,
// la de más ahorro que cumple su parada, la distancia del nivel, otro negocio (y otro tipo, salvo «Antojo dulce") y
// estar abierta a su hora. Devuelve la mejor ruta de las anclas probadas.
func buildThemed(th planTheme, level string, list, services []MarketCandidate, used map[uuid.UUID]bool, start time.Time) *PlanRoute {
	var best *PlanRoute
	anchors := 0
	for _, a := range list {
		if anchors == planAnchors {
			break
		}
		if used[a.Offer.ID] || !slotOK(th.Slots[0], a) {
			continue
		}
		okA, whenA := stopAt(a, level, start)
		if !okA {
			continue
		}
		anchors++
		stops := []PlanStop{{Cand: a, At: whenA}}
		kinds := map[string]bool{kindOf(a): true}
		ents := map[uuid.UUID]bool{a.Offer.EntityID: true}
		prev, prevAt := a, start
		ok := true
		for si := 1; si < len(th.Slots); si++ {
			var pick *MarketCandidate
			var when *time.Time
			var pickAt time.Time
			// Primero las ofertas (van por ahorro: la primera que cumple es la mejor); si ninguna llena la parada, un
			// servicio de un negocio con buenas reseñas.
			for _, pool := range [][]MarketCandidate{list, services} {
				for i := range pool {
					b := &pool[i]
					if used[b.Offer.ID] || ents[b.Offer.EntityID] || !slotOK(th.Slots[si], *b) || !nearFor(level, a, *b) {
						continue
					}
					if !th.SameKindOK && kinds[kindOf(*b)] {
						continue
					}
					at := nextAt(prev, prevAt, si == 1, *b)
					okB, w := stopAt(*b, level, at)
					if !okB {
						continue
					}
					pick, when, pickAt = b, w, at
					break
				}
				if pick != nil {
					break
				}
			}
			if pick == nil {
				if th.Optional && si == len(th.Slots)-1 {
					break
				}
				ok = false
				break
			}
			stops = append(stops, PlanStop{Cand: *pick, At: when})
			kinds[kindOf(*pick)] = true
			ents[pick.Offer.EntityID] = true
			prev, prevAt = *pick, pickAt
		}
		if !ok || len(stops) < 2 {
			continue
		}
		r := finishRoute(th.Key, level, stops)
		if best == nil || r.Saving > best.Saving {
			best = &r
		}
	}
	return best
}

func finishRoute(theme, level string, stops []PlanStop) PlanRoute {
	r := PlanRoute{Theme: theme, Level: level, Stops: stops, ZoneID: stops[0].Cand.Offer.Entity.ZoneID}
	for i, s := range stops {
		r.Saving += SavingPesos(s.Cand)
		if i > 0 {
			r.WalkM += entityDistance(stops[i-1].Cand, s.Cand)
		}
	}
	return r
}

// buildDay arma «tu día en la ciudad»: para cada momento que queda hoy (9, 15 y 19 h), la oferta de más ahorro de un
// tipo nuevo que está abierta a esa hora. Si hoy ya no quedan dos momentos (de noche), el plan es para mañana; la app
// lo nombra por el día de la primera parada («Tu domingo en Santa Marta»).
func buildDay(list []MarketCandidate, used map[uuid.UUID]bool, now time.Time) *PlanRoute {
	if r := buildDayOn(list, used, now, now); r != nil {
		return r
	}
	local := now.In(marketTZ)
	y, m, d := local.Date()
	return buildDayOn(list, used, now, time.Date(y, m, d+1, 0, 0, 0, 0, marketTZ))
}

func buildDayOn(list []MarketCandidate, used map[uuid.UUID]bool, now, day time.Time) *PlanRoute {
	local := now.In(marketTZ)
	y, m, d := day.In(marketTZ).Date()
	stops := []PlanStop{}
	kinds := map[string]bool{}
	ents := map[uuid.UUID]bool{}
	for _, h := range dayMoments {
		at := time.Date(y, m, d, h, 0, 0, 0, marketTZ)
		if !at.After(local) {
			continue
		}
		for _, c := range list {
			if used[c.Offer.ID] || ents[c.Offer.EntityID] || kinds[kindOf(c)] || !openAt(c, at) {
				continue
			}
			t := at
			stops = append(stops, PlanStop{Cand: c, At: &t})
			kinds[kindOf(c)] = true
			ents[c.Offer.EntityID] = true
			break
		}
	}
	if len(stops) < 2 {
		return nil
	}
	r := finishRoute(ThemeDay, PlanLevelDay, stops)
	return &r
}

// BuildPlans arma las rutas del barrio (3.49): con ubicación, un plan por tema a pie o en tu barrio; si ningún tema
// sale, «Ruta por {zona}» con cualquier mezcla (la de 3.48); y si tampoco, o sin ubicación, «tu día en la ciudad».
// Una oferta va en una sola ruta; se ordenan por el ahorro sumado.
func BuildPlans(cands []MarketCandidate, opt PlanOptions) []PlanRoute {
	if opt.Max <= 0 {
		opt.Max = RoutesMax
	}
	list := make([]MarketCandidate, 0, len(cands))
	for _, c := range cands {
		if SavingPesos(c) > 0 && (c.Offer.Entity.Latitude != 0 || c.Offer.Entity.Longitude != 0) {
			list = append(list, c)
		}
	}
	betterFirst(list, opt.HasLocation)
	services := append([]MarketCandidate(nil), opt.Services...)
	betterFirst(services, opt.HasLocation)
	used := map[uuid.UUID]bool{}
	take := func(r *PlanRoute) {
		for _, s := range r.Stops {
			used[s.Cand.Offer.ID] = true
		}
	}
	out := []PlanRoute{}
	if opt.HasLocation {
		for _, th := range planThemes {
			start := planStart(opt.Now, th.From)
			if !themeApplies(th, start) {
				continue
			}
			for _, level := range []string{PlanLevelWalk, PlanLevelZone} {
				if r := buildThemed(th, level, list, services, used, start); r != nil {
					take(r)
					out = append(out, *r)
					break
				}
			}
		}
		if len(out) == 0 {
			// Ningún tema encaja: la mezcla de siempre (tres tipos distintos a pie), sin hora, con lo que está abierto
			// ahora o no tiene horario.
			openNow := []MarketCandidate{}
			start := planStart(opt.Now, 0)
			for _, c := range list {
				if ok, _ := stopAt(c, PlanLevelWalk, start); ok {
					openNow = append(openNow, c)
				}
			}
			for _, r := range BuildRoutes(openNow, 1, opt.HasLocation) {
				// Un orden en que cada parada esté abierta a su turno (ahora, +1 h, +30 min); de los que sirven, el de
				// menos recorrido. Si ninguno sirve, la ruta no sale.
				if stops := timedOrder(r.Stops, start); stops != nil {
					pr := finishRoute(ThemeZone, PlanLevelWalk, stops)
					take(&pr)
					out = append(out, pr)
				}
			}
		}
	}
	if len(out) == 0 {
		if r := buildDay(list, used, opt.Now); r != nil {
			out = append(out, *r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Saving > out[j].Saving })
	if len(out) > opt.Max {
		out = out[:opt.Max]
	}
	return out
}

// timedOrder prueba los órdenes posibles de las paradas (son 3: 6 órdenes) y devuelve, de los que dejan cada una
// abierta a su turno, el de menos recorrido, con la hora de cada parada. nil si ninguno sirve.
func timedOrder(cands []MarketCandidate, start time.Time) []PlanStop {
	var best []PlanStop
	bestWalk := 0.0
	var try func(rest []MarketCandidate, done []MarketCandidate)
	try = func(rest []MarketCandidate, done []MarketCandidate) {
		if len(rest) == 0 {
			stops := make([]PlanStop, 0, len(done))
			at := start
			walk := 0.0
			for i, c := range done {
				ok, when := stopAt(c, PlanLevelWalk, at)
				if !ok {
					return
				}
				stops = append(stops, PlanStop{Cand: c, At: when})
				if i > 0 {
					walk += entityDistance(done[i-1], c)
				}
				if i == 0 {
					at = at.Add(planFirstGap)
				} else {
					at = at.Add(planNextGap)
				}
			}
			if best == nil || walk < bestWalk {
				best, bestWalk = stops, walk
			}
			return
		}
		for i := range rest {
			next := append(append([]MarketCandidate(nil), rest[:i]...), rest[i+1:]...)
			try(next, append(append([]MarketCandidate(nil), done...), rest[i]))
		}
	}
	try(cands, nil)
	return best
}

// NextStops es «Completa tu plan» (3.49): hasta 2 vecinos para después de reclamar `from`, de otro tipo y otro
// negocio, abiertos dentro de una hora (o sin horario), a menos de 1 km del negocio; si no hay, de su misma zona. El
// más cercano primero; empate, el de más ahorro.
func NextStops(from MarketCandidate, cands []MarketCandidate, now time.Time, max int) []MarketCandidate {
	at := now.Add(planFirstGap)
	pick := func(level string) []MarketCandidate {
		out := []MarketCandidate{}
		kinds := map[string]bool{kindOf(from): true}
		ents := map[uuid.UUID]bool{from.Offer.EntityID: true}
		list := append([]MarketCandidate(nil), cands...)
		sort.SliceStable(list, func(i, j int) bool {
			di, dj := entityDistance(from, list[i]), entityDistance(from, list[j])
			if di != dj {
				return di < dj
			}
			return SavingPesos(list[i]) > SavingPesos(list[j])
		})
		for _, c := range list {
			if len(out) == max {
				break
			}
			if ents[c.Offer.EntityID] || kinds[kindOf(c)] || SavingPesos(c) <= 0 || !nearFor(level, from, c) {
				continue
			}
			if len(c.Offer.Entity.Hours) > 0 && !openAt(c, at) {
				continue
			}
			out = append(out, c)
			kinds[kindOf(c)] = true
			ents[c.Offer.EntityID] = true
		}
		return out
	}
	if r := pick(PlanLevelWalk); len(r) > 0 {
		return r
	}
	return pick(PlanLevelZone)
}

// StopPrice es lo que se paga en una parada (3.50): el precio especial de la oferta (o el precio escrito, en ofertas de
// antes) o el precio de siempre del servicio. 0 si no se sabe.
func StopPrice(c MarketCandidate) int {
	if c.Service != nil {
		if c.Service.Price != nil {
			return *c.Service.Price
		}
		return 0
	}
	if c.Offer.SpecialPrice != nil {
		return *c.Offer.SpecialPrice
	}
	n := 0
	for _, r := range c.Offer.Price {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}
