package services

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Ofertas como marketplace (3.38, mockup ofertas_marketplace.html). Este archivo es la parte pura: con las ofertas
// vigentes ya cargadas y sus señales (distancia, tendencia, valoración, por qué te la recomendamos), arma las secciones
// en el orden pedido —cerca, lo más buscado, tendencia, recomendado— sin repetir una oferta entre secciones. Nada
// patrocinado, ninguna ciudad por encima de otra (todo sale de dónde está la persona) y sin conteos de ofertas.

// 3.39 (mockup ofertas_ciudad.html): la vitrina es la ciudad donde está la persona, no un radio a su alrededor. La
// ubicación decide la ciudad y ordena; la lista completa se pagina en lugar de cortarse.

const (
	MarketNearRadiusM = 3000  // «Cerca de ti»
	MarketCityRadiusM = 30000 // a más de esto de cualquier zona o negocio, no se adivina ciudad
	MarketRailMax     = 8     // tarjetas por sección
	MarketPageSize    = 20    // tanda de «Todas las ofertas de la ciudad»
	MarketMinRail     = 2     // una sección de lado con menos se esconde («Recomendado» basta con 1)
	MarketTrendMin    = 2.0   // puntaje mínimo para decir «en tendencia»
	MarketZoneLooks   = 3     // categorías que dibuja la tarjeta de una zona
)

// MarketCandidate es una oferta vigente con lo que se sabe de ella para esta persona.
type MarketCandidate struct {
	Offer     models.Offer
	DistanceM float64 // < 0 = sin ubicación
	Trend     float64 // reclamos y vistas recientes (ponderados)
	Rating    float64
	Reason    string // por qué es para ti («Sigues a …»); vacío = no es personal
	Open      bool   // abierto ahora según su horario (3.48)
	// Service: si no es nil, la candidata es un servicio del catálogo de un negocio sin oferta que entra a una ruta
	// por sus reseñas (3.50). Offer lleva entonces el negocio y el nombre del servicio, sin precio especial ni ahorro.
	Service  *models.ServiceItem
	Bookable bool // el negocio usa la agenda y el servicio se puede pedir con hora
}

type MarketSections struct {
	Near     []MarketCandidate
	Searched []MarketCandidate
	Trending []MarketCandidate
	ForYou   []MarketCandidate
	// TopRated: la sección personal se llena con las mejor valoradas (sin sesión o sin historia).
	TopRated bool
}

type MarketOptions struct {
	HasLocation bool
	Term        string // término elegido de «Lo más buscado»
	Personal    bool   // hay sesión: la sección personal usa Reason; si no, valoración
}

// CityPoint es un negocio con oferta vigente, para deducir la ciudad cuando no hay zona cerca.
type CityPoint struct {
	City      string
	Lat, Long float64
}

// SameCity compara nombres de ciudad sin mayúsculas, tildes ni espacios de más («santa marta» = «Santa Marta»).
func SameCity(a, b string) bool {
	return FoldText(a) != "" && FoldText(a) == FoldText(b)
}

// ResolveCity decide de qué ciudad es la vitrina. Con ubicación manda ella: la zona que te contiene; si no, la zona más
// cercana; si no hay zonas cerca, el negocio con oferta más cercano. Nada a menos de MarketCityRadiusM = sin ciudad
// (se ve todo, ordenado por distancia). Sin ubicación se usa la última ciudad conocida que manda la app (`hint`), solo
// si existe. Nunca hay una ciudad por defecto: ninguna va por encima de otra. `here` es la zona que te contiene.
func ResolveCity(zones []models.Zone, points []CityPoint, lat, long float64, hasLoc bool, hint string) (city string, here *models.Zone) {
	if !hasLoc {
		for _, z := range zones {
			if SameCity(z.City, hint) {
				return z.City, nil
			}
		}
		for _, p := range points {
			if SameCity(p.City, hint) {
				return p.City, nil
			}
		}
		return "", nil
	}
	if z := NearestZone(zones, lat, long); z != nil {
		return z.City, z
	}
	best, bestD := "", float64(MarketCityRadiusM)
	for _, z := range zones {
		if !z.Active || strings.TrimSpace(z.City) == "" {
			continue
		}
		if d := utils.DistanceMeters(lat, long, z.Latitude, z.Longitude); d <= bestD {
			best, bestD = z.City, d
		}
	}
	if best != "" {
		return best, nil
	}
	for _, p := range points {
		if strings.TrimSpace(p.City) == "" {
			continue
		}
		if d := utils.DistanceMeters(lat, long, p.Lat, p.Long); d <= bestD {
			best, bestD = p.City, d
		}
	}
	return strings.TrimSpace(best), nil
}

// InCity deja solo las ofertas de esa ciudad (ciudad vacía = todas).
func InCity(cands []MarketCandidate, city string) []MarketCandidate {
	if strings.TrimSpace(city) == "" {
		return cands
	}
	out := make([]MarketCandidate, 0, len(cands))
	for _, c := range cands {
		if SameCity(c.Offer.Entity.City, city) {
			out = append(out, c)
		}
	}
	return out
}

// MarketZone es una tarjeta de «Por zona»: la zona, qué tan lejos queda, si estás en ella y hasta 3 categorías de lo
// que hay (para el mosaico). Sin conteos.
type MarketZone struct {
	Zone      models.Zone
	DistanceM float64 // < 0 = sin ubicación
	Here      bool
	Looks     []models.Category
}

// BuildZones arma «Por zona»: solo zonas de la ciudad con al menos una oferta (de las ya filtradas). Con ubicación, la
// más cercana primero; sin ella, en orden alfabético.
func BuildZones(zones []models.Zone, cands []MarketCandidate, city string, here *models.Zone, lat, long float64, hasLoc bool) []MarketZone {
	out := []MarketZone{}
	if strings.TrimSpace(city) == "" {
		return out
	}
	for _, z := range zones {
		if !z.Active || !SameCity(z.City, city) {
			continue
		}
		seen := map[uuid.UUID]bool{}
		looks := []models.Category{}
		for _, c := range cands {
			e := c.Offer.Entity
			if e.ZoneID == nil || *e.ZoneID != z.ID {
				continue
			}
			if !seen[e.CategoryID] && len(looks) < MarketZoneLooks {
				seen[e.CategoryID] = true
				looks = append(looks, e.Category)
			}
		}
		if len(looks) == 0 {
			continue
		}
		d := -1.0
		if hasLoc {
			d = utils.DistanceMeters(lat, long, z.Latitude, z.Longitude)
		}
		out = append(out, MarketZone{Zone: z, DistanceM: d, Here: here != nil && here.ID == z.ID, Looks: looks})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Here != out[j].Here {
			return out[i].Here
		}
		if hasLoc && out[i].DistanceM != out[j].DistanceM {
			return out[i].DistanceM < out[j].DistanceM
		}
		return FoldText(out[i].Zone.Name) < FoldText(out[j].Zone.Name)
	})
	return out
}

// AllOptions: la lista completa, con zona, búsqueda y orden.
type AllOptions struct {
	HasLocation bool
	Zone        *uuid.UUID
	Query       string
	Sort        string // near | ends | save («near» sin ubicación = ends)
}

// PageAll ordena y corta una tanda de la lista completa. `next` es el desplazamiento de la siguiente tanda, -1 al
// final. El orden desempata por id para que las tandas no repitan ni salten ofertas.
func PageAll(cands []MarketCandidate, opt AllOptions, offset, size int) (page []MarketCandidate, next int) {
	list := []MarketCandidate{}
	for _, c := range cands {
		if opt.Zone != nil && (c.Offer.Entity.ZoneID == nil || *c.Offer.Entity.ZoneID != *opt.Zone) {
			continue
		}
		if strings.TrimSpace(opt.Query) != "" && !MatchesTerm(c.Offer, opt.Query) {
			continue
		}
		list = append(list, c)
	}
	key := opt.Sort
	if key == "near" && !opt.HasLocation {
		key = "ends"
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		switch key {
		case "save":
			if sa, sb := offerSaving(a.Offer), offerSaving(b.Offer); sa != sb {
				return sa > sb
			}
		case "near":
			if a.DistanceM != b.DistanceM {
				return a.DistanceM < b.DistanceM
			}
		}
		if !a.Offer.EndsAt.Equal(b.Offer.EndsAt) {
			return a.Offer.EndsAt.Before(b.Offer.EndsAt)
		}
		return a.Offer.ID.String() < b.Offer.ID.String()
	})
	if size <= 0 {
		size = MarketPageSize
	}
	if offset < 0 || offset >= len(list) {
		return []MarketCandidate{}, -1
	}
	end := offset + size
	if end >= len(list) {
		return list[offset:], -1
	}
	return list[offset:end], end
}

// FoldText: minúsculas, sin tildes ni signos, espacios simples. Para comparar términos con ofertas.
func FoldText(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, strings.ToLower(s))
	if err != nil {
		out = strings.ToLower(s)
	}
	var b strings.Builder
	space := false
	for _, r := range out {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteRune(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// MatchesTerm: cada palabra del término aparece (como inicio de palabra) en el título, el negocio, la categoría o los
// servicios incluidos. «barberia» encuentra «Barbería El Parque»; «corte» encuentra «Corte + barba».
func MatchesTerm(o models.Offer, term string) bool {
	words := strings.Fields(FoldText(term))
	if len(words) == 0 {
		return false
	}
	parts := []string{o.Title, o.Entity.Name, o.Entity.Category.Name}
	for _, it := range o.Items {
		parts = append(parts, it.Name)
	}
	hay := " " + FoldText(strings.Join(parts, " ")) + " "
	for _, w := range words {
		if !strings.Contains(hay, " "+w) {
			return false
		}
	}
	return true
}

func offerSaving(o models.Offer) float64 {
	if o.SpecialPrice == nil || o.ReferencePrice == nil || *o.ReferencePrice <= 0 {
		return 0
	}
	return 1 - float64(*o.SpecialPrice)/float64(*o.ReferencePrice)
}

// BuildMarket reparte las ofertas (ya de la ciudad) en secciones. Al repartir van primero el término elegido y lo
// personal (si no, quedaban vacíos porque «cerca» ya había tomado sus ofertas); el orden en pantalla lo decide la app.
// La lista completa va aparte (PageAll) y sí incluye lo de arriba.
func BuildMarket(cands []MarketCandidate, opt MarketOptions) MarketSections {
	var out MarketSections
	used := map[uuid.UUID]bool{}
	take := func(list []MarketCandidate, n int) []MarketCandidate {
		res := []MarketCandidate{}
		for _, c := range list {
			if len(res) == n {
				break
			}
			if used[c.Offer.ID] {
				continue
			}
			used[c.Offer.ID] = true
			res = append(res, c)
		}
		return res
	}
	byDistance := func(list []MarketCandidate) []MarketCandidate {
		s := append([]MarketCandidate(nil), list...)
		sort.SliceStable(s, func(i, j int) bool {
			if opt.HasLocation && s[i].DistanceM != s[j].DistanceM {
				return s[i].DistanceM < s[j].DistanceM
			}
			return s[i].Offer.EndsAt.Before(s[j].Offer.EndsAt)
		})
		return s
	}
	inZone := cands // ya filtradas por ciudad

	// 1. El término elegido.
	if opt.Term != "" {
		m := []MarketCandidate{}
		for _, c := range inZone {
			if MatchesTerm(c.Offer, opt.Term) {
				m = append(m, c)
			}
		}
		out.Searched = take(byDistance(m), MarketRailMax)
	}
	// 2. Lo personal: con sesión, lo que tiene motivo; si no hay nada, las mejor valoradas (con al menos una reseña).
	personal := []MarketCandidate{}
	if opt.Personal {
		for _, c := range inZone {
			if c.Reason != "" {
				personal = append(personal, c)
			}
		}
	}
	if len(personal) > 0 {
		out.ForYou = take(byDistance(personal), MarketRailMax)
	} else {
		rated := []MarketCandidate{}
		for _, c := range inZone {
			if c.Rating > 0 {
				rated = append(rated, c)
			}
		}
		sort.SliceStable(rated, func(i, j int) bool { return rated[i].Rating > rated[j].Rating })
		out.ForYou = take(rated, MarketRailMax)
		out.TopRated = true
		for i := range out.ForYou {
			out.ForYou[i].Reason = ""
		}
	}
	// 3. Cerca de ti (solo con ubicación).
	if opt.HasLocation {
		near := []MarketCandidate{}
		for _, c := range inZone {
			if c.DistanceM <= MarketNearRadiusM {
				near = append(near, c)
			}
		}
		out.Near = take(byDistance(near), MarketRailMax)
	}
	// 4. Tendencia.
	hot := []MarketCandidate{}
	for _, c := range inZone {
		if c.Trend >= MarketTrendMin {
			hot = append(hot, c)
		}
	}
	sort.SliceStable(hot, func(i, j int) bool { return hot[i].Trend > hot[j].Trend })
	out.Trending = take(hot, MarketRailMax)

	// Secciones de lado con muy poco no se muestran.
	if len(out.Near) < MarketMinRail {
		out.Near = nil
	}
	if len(out.Trending) < MarketMinRail {
		out.Trending = nil
	}
	return out
}

// marketTZ: «hoy» es el día en Colombia (el servidor puede correr en UTC).
var marketTZ = func() *time.Location {
	if l, err := time.LoadLocation("America/Bogota"); err == nil {
		return l
	}
	return time.FixedZone("COT", -5*3600)
}()

// PickFeatured es la destacada de la vitrina (3.40): la de mayor ahorro entre las que terminan hoy. Es una regla a la
// vista («Mayor ahorro de hoy»), nunca pagada. Sin ahorro real no hay destacada; empate: la que termina antes.
func PickFeatured(cands []MarketCandidate, now time.Time) *MarketCandidate {
	y, m, d := now.In(marketTZ).Date()
	endOfDay := time.Date(y, m, d+1, 0, 0, 0, 0, marketTZ)
	var best *MarketCandidate
	for i := range cands {
		c := &cands[i]
		if !c.Offer.EndsAt.After(now) || !c.Offer.EndsAt.Before(endOfDay) || offerSaving(c.Offer) <= 0 {
			continue
		}
		if best == nil {
			best = c
			continue
		}
		sb, sc := offerSaving(best.Offer), offerSaving(c.Offer)
		if sc > sb || (sc == sb && c.Offer.EndsAt.Before(best.Offer.EndsAt)) {
			best = c
		}
	}
	return best
}

// WithoutOffer devuelve las candidatas sin esa oferta (la destacada no se repite en las secciones).
func WithoutOffer(cands []MarketCandidate, id uuid.UUID) []MarketCandidate {
	out := make([]MarketCandidate, 0, len(cands))
	for _, c := range cands {
		if c.Offer.ID != id {
			out = append(out, c)
		}
	}
	return out
}
