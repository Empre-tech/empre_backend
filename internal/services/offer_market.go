package services

import (
	"sort"
	"strings"
	"unicode"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Ofertas como marketplace (3.38, mockup ofertas_marketplace.html). Este archivo es la parte pura: con las ofertas
// vigentes ya cargadas y sus señales (distancia, tendencia, valoración, por qué te la recomendamos), arma las secciones
// en el orden pedido —cerca, lo más buscado, tendencia, recomendado— sin repetir una oferta entre secciones. Nada
// patrocinado, ninguna ciudad por encima de otra (todo sale de dónde está la persona) y sin conteos de ofertas.

const (
	MarketNearRadiusM = 3000  // «Cerca de ti»
	MarketZoneRadiusM = 10000 // «Todas»: la zona, no el país
	MarketRailMax     = 8     // tarjetas por sección
	MarketAllMax      = 40
	MarketMinRail     = 2   // una sección de lado con menos se esconde («Recomendado» basta con 1)
	MarketTrendMin    = 2.0 // puntaje mínimo para decir «en tendencia»
)

// MarketCandidate es una oferta vigente con lo que se sabe de ella para esta persona.
type MarketCandidate struct {
	Offer     models.Offer
	DistanceM float64 // < 0 = sin ubicación
	Trend     float64 // reclamos y vistas recientes (ponderados)
	Rating    float64
	Reason    string // por qué es para ti («Sigues a …»); vacío = no es personal
}

type MarketSections struct {
	Near     []MarketCandidate
	Searched []MarketCandidate
	Trending []MarketCandidate
	ForYou   []MarketCandidate
	// TopRated: la sección personal se llena con las mejor valoradas (sin sesión o sin historia).
	TopRated bool
	All      []MarketCandidate
}

type MarketOptions struct {
	HasLocation bool
	Term        string // término elegido de «Lo más buscado»
	Sort        string // near | ends | save
	Personal    bool   // hay sesión: la sección personal usa Reason; si no, valoración
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

// BuildMarket reparte las ofertas en secciones. Al repartir van primero el término elegido y lo personal (si no,
// quedaban vacíos porque «cerca» ya había tomado sus ofertas); el orden en pantalla lo decide la app. «Todas» es el
// listado completo de la zona y sí puede repetir lo de arriba.
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
	inZone := []MarketCandidate{}
	for _, c := range cands {
		if !opt.HasLocation || c.DistanceM <= MarketZoneRadiusM {
			inZone = append(inZone, c)
		}
	}

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

	// Secciones de lado con muy poco no se muestran (se devuelven las ofertas al resto, para «Todas»).
	if len(out.Near) < MarketMinRail {
		out.Near = nil
	}
	if len(out.Trending) < MarketMinRail {
		out.Trending = nil
	}

	// Todas: la zona completa con el orden elegido.
	all := append([]MarketCandidate(nil), inZone...)
	switch opt.Sort {
	case "save":
		sort.SliceStable(all, func(i, j int) bool { return offerSaving(all[i].Offer) > offerSaving(all[j].Offer) })
	case "ends":
		sort.SliceStable(all, func(i, j int) bool { return all[i].Offer.EndsAt.Before(all[j].Offer.EndsAt) })
	default:
		all = byDistance(all)
	}
	if len(all) > MarketAllMax {
		all = all[:MarketAllMax]
	}
	out.All = all
	return out
}
