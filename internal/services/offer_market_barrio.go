package services

import (
	"sort"
	"time"

	"empre_backend/pkg/utils"

	"github.com/google/uuid"
)

// Ofertas · el barrio (3.48, mockup ofertas_donde_buscar.html versión 4). Parte pura, como offer_market.go: con las
// candidatas ya de la ciudad arma la pasarela (tres reglas a la vista), el mosaico (un vecino de cada tipo), las
// rutas (tres vecinos distintos cerca entre sí) y «Tus vecinos hoy» (un negocio por círculo). Nada pagado ni conteos.

const (
	SpotlightNewWindow = 7 * 24 * time.Hour // «Nueva esta semana»
	BentoMax           = 4                  // piezas pequeñas del mosaico
	RouteStops         = 3
	RouteRadiusM       = 1000 // cada parada a menos de 10 cuadras de la primera
	RoutesMax          = 3
	NeighborsMax       = 12
)

// Reglas de la pasarela, en el orden en que se muestran.
const (
	SpotlightSaveToday = "save_today" // «Mayor ahorro de hoy» (la destacada de 3.40)
	SpotlightEndsFirst = "ends_first" // «La que termina antes»
	SpotlightNewWeek   = "new_week"   // «Nueva esta semana en tu barrio»
)

type SpotlightSlide struct {
	Rule string
	Cand MarketCandidate
}

// SavingPesos es el ahorro en pesos (referencia congelada menos el precio especial); 0 sin ahorro real.
func SavingPesos(c MarketCandidate) int {
	o := c.Offer
	if o.SpecialPrice == nil || o.ReferencePrice == nil || *o.ReferencePrice <= *o.SpecialPrice {
		return 0
	}
	return *o.ReferencePrice - *o.SpecialPrice
}

// PickSpotlight arma la pasarela: hasta tres reglas, cada una con una oferta y un tipo de negocio distintos (la
// variedad se ve también aquí). Una regla sin candidata no sale. La primera es la destacada de PickFeatured.
func PickSpotlight(cands []MarketCandidate, now time.Time) []SpotlightSlide {
	out := []SpotlightSlide{}
	usedOffer := map[uuid.UUID]bool{}
	usedCat := map[uuid.UUID]bool{}
	add := func(rule string, c *MarketCandidate) {
		if c == nil {
			return
		}
		usedOffer[c.Offer.ID] = true
		usedCat[c.Offer.Entity.CategoryID] = true
		out = append(out, SpotlightSlide{Rule: rule, Cand: *c})
	}
	free := func(c *MarketCandidate) bool {
		return !usedOffer[c.Offer.ID] && !usedCat[c.Offer.Entity.CategoryID] && SavingPesos(*c) > 0
	}

	add(SpotlightSaveToday, PickFeatured(cands, now))

	var first *MarketCandidate
	for i := range cands {
		c := &cands[i]
		if !free(c) || !c.Offer.EndsAt.After(now) {
			continue
		}
		if first == nil || c.Offer.EndsAt.Before(first.Offer.EndsAt) {
			first = c
		}
	}
	add(SpotlightEndsFirst, first)

	var newest *MarketCandidate
	for i := range cands {
		c := &cands[i]
		if !free(c) || c.Offer.CreatedAt.Before(now.Add(-SpotlightNewWindow)) {
			continue
		}
		if newest == nil || c.Offer.CreatedAt.After(newest.Offer.CreatedAt) {
			newest = c
		}
	}
	add(SpotlightNewWeek, newest)
	return out
}

// barrioFirst: con ubicación, lo que está a menos de MarketNearRadiusM va antes (es «tu barrio»); el resto después.
func barrioFirst(c MarketCandidate, hasLoc bool) bool {
	return hasLoc && c.DistanceM >= 0 && c.DistanceM <= MarketNearRadiusM
}

// PickBento elige el mosaico: el mayor ahorro de cada tipo de negocio (categoría), sin repetir tipo ni negocio, y
// sin las ofertas de `skip` (la pasarela). Con ubicación, primero lo de tu barrio. El ahorro se compara en
// porcentaje para que no ganen siempre las ofertas caras. Solo ofertas con ahorro real: la pieza muestra «−$7.000».
func PickBento(cands []MarketCandidate, skip map[uuid.UUID]bool, n int, hasLoc bool) []MarketCandidate {
	list := make([]MarketCandidate, 0, len(cands))
	for _, c := range cands {
		if !skip[c.Offer.ID] && SavingPesos(c) > 0 {
			list = append(list, c)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if bi, bj := barrioFirst(list[i], hasLoc), barrioFirst(list[j], hasLoc); bi != bj {
			return bi
		}
		if si, sj := offerSaving(list[i].Offer), offerSaving(list[j].Offer); si != sj {
			return si > sj
		}
		return list[i].Offer.EndsAt.Before(list[j].Offer.EndsAt)
	})
	out := []MarketCandidate{}
	cats := map[uuid.UUID]bool{}
	ents := map[uuid.UUID]bool{}
	for _, c := range list {
		if len(out) == n {
			break
		}
		if cats[c.Offer.Entity.CategoryID] || ents[c.Offer.EntityID] {
			continue
		}
		cats[c.Offer.Entity.CategoryID] = true
		ents[c.Offer.EntityID] = true
		out = append(out, c)
	}
	return out
}

// Route es un plan con tres vecinos de tipos distintos, cerca entre sí. WalkM es el recorrido entre paradas en orden.
type Route struct {
	Stops  []MarketCandidate
	Saving int
	WalkM  float64
}

func entityDistance(a, b MarketCandidate) float64 {
	return utils.DistanceMeters(a.Offer.Entity.Latitude, a.Offer.Entity.Longitude, b.Offer.Entity.Latitude, b.Offer.Entity.Longitude)
}

// BuildRoutes arma las rutas del barrio. Cada ruta parte de la oferta con más ahorro que aún no está en otra ruta y
// suma las dos mejores que quedan a menos de RouteRadiusM de ella, de otro tipo de negocio y de otro negocio. Las
// paradas van en orden de cercanía (de la primera a la más próxima, y de ahí a la última). Una oferta va en una sola
// ruta. Con ubicación, las que parten de tu barrio van primero. Las rutas pueden repetir ofertas de los carruseles: son planes armados, no otra sección.
func BuildRoutes(cands []MarketCandidate, max int, hasLoc bool) []Route {
	list := make([]MarketCandidate, 0, len(cands))
	for _, c := range cands {
		if SavingPesos(c) > 0 && (c.Offer.Entity.Latitude != 0 || c.Offer.Entity.Longitude != 0) {
			list = append(list, c)
		}
	}
	// Con ubicación, las rutas de tu barrio primero.
	sort.SliceStable(list, func(i, j int) bool {
		if bi, bj := barrioFirst(list[i], hasLoc), barrioFirst(list[j], hasLoc); bi != bj {
			return bi
		}
		return SavingPesos(list[i]) > SavingPesos(list[j])
	})
	used := map[uuid.UUID]bool{}
	out := []Route{}
	for _, a := range list {
		if len(out) == max {
			break
		}
		if used[a.Offer.ID] {
			continue
		}
		stops := []MarketCandidate{a}
		cats := map[uuid.UUID]bool{a.Offer.Entity.CategoryID: true}
		ents := map[uuid.UUID]bool{a.Offer.EntityID: true}
		for _, b := range list {
			if len(stops) == RouteStops {
				break
			}
			if used[b.Offer.ID] || cats[b.Offer.Entity.CategoryID] || ents[b.Offer.EntityID] || entityDistance(a, b) > RouteRadiusM {
				continue
			}
			cats[b.Offer.Entity.CategoryID] = true
			ents[b.Offer.EntityID] = true
			stops = append(stops, b)
		}
		if len(stops) < RouteStops {
			continue
		}
		// Orden de paso: la primera, luego la más cercana a ella, luego la otra.
		if entityDistance(stops[0], stops[2]) < entityDistance(stops[0], stops[1]) {
			stops[1], stops[2] = stops[2], stops[1]
		}
		r := Route{Stops: stops, WalkM: entityDistance(stops[0], stops[1]) + entityDistance(stops[1], stops[2])}
		for _, s := range stops {
			used[s.Offer.ID] = true
			r.Saving += SavingPesos(s)
		}
		out = append(out, r)
	}
	return out
}

// Neighbor es un círculo de «Tus vecinos hoy»: un negocio con su mejor oferta y cuánto le queda (0 a 1) a esa oferta.
type Neighbor struct {
	Cand     MarketCandidate
	TimeLeft float64
}

// BuildNeighbors: un círculo por negocio, con su oferta de más ahorro (o la que termina antes si ninguna ahorra). Con
// ubicación, el más cercano primero; sin ella, el que termina antes.
func BuildNeighbors(cands []MarketCandidate, hasLoc bool, now time.Time, max int) []Neighbor {
	best := map[uuid.UUID]MarketCandidate{}
	order := []uuid.UUID{}
	for _, c := range cands {
		cur, ok := best[c.Offer.EntityID]
		if !ok {
			order = append(order, c.Offer.EntityID)
			best[c.Offer.EntityID] = c
			continue
		}
		if s, sc := SavingPesos(c), SavingPesos(cur); s > sc || (s == sc && c.Offer.EndsAt.Before(cur.Offer.EndsAt)) {
			best[c.Offer.EntityID] = c
		}
	}
	out := make([]Neighbor, 0, len(order))
	for _, id := range order {
		c := best[id]
		left := 0.0
		if span := c.Offer.EndsAt.Sub(c.Offer.StartsAt); span > 0 {
			left = float64(c.Offer.EndsAt.Sub(now)) / float64(span)
		}
		if left < 0 {
			left = 0
		} else if left > 1 {
			left = 1
		}
		out = append(out, Neighbor{Cand: c, TimeLeft: left})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Cand, out[j].Cand
		if hasLoc && a.DistanceM != b.DistanceM {
			return a.DistanceM < b.DistanceM
		}
		return a.Offer.EndsAt.Before(b.Offer.EndsAt)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}
