package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

func mkOffer(title, biz, cat string, endsIn time.Duration, special, ref int) models.Offer {
	sp, rf := special, ref
	return models.Offer{ID: uuid.New(), Title: title, EndsAt: time.Now().Add(endsIn), SpecialPrice: &sp, ReferencePrice: &rf,
		Entity: models.Entity{Name: biz, Category: models.Category{Name: cat}}}
}

func marketIDs(list []MarketCandidate) map[uuid.UUID]bool {
	m := map[uuid.UUID]bool{}
	for _, c := range list {
		m[c.Offer.ID] = true
	}
	return m
}

func TestFoldAndMatch(t *testing.T) {
	if FoldText("  Barbería   El-Parque! ") != "barberia el parque" {
		t.Fatalf("fold: %q", FoldText("  Barbería   El-Parque! "))
	}
	o := mkOffer("Corte + barba", "Barbería El Parque", "Belleza y peluquería", time.Hour, 28000, 37000)
	o.Items = []models.OfferItem{{Name: "Arreglo de barba"}}
	for term, want := range map[string]bool{"barberia": true, "corte": true, "Arreglo": true, "corte barba": true, "almuerzo": false, "arte": false} {
		if MatchesTerm(o, term) != want {
			t.Errorf("MatchesTerm(%q) = %v", term, !want)
		}
	}
}

func TestBuildMarket(t *testing.T) {
	alm := MarketCandidate{Offer: mkOffer("Almuerzo ejecutivo", "La Sazón", "Restaurantes", 2*time.Hour, 18000, 22000), DistanceM: 350, Rating: 4.7}
	corte := MarketCandidate{Offer: mkOffer("Corte + barba", "Barbería El Parque", "Belleza", 7*time.Hour, 28000, 37000), DistanceM: 600, Trend: 3, Reason: "Sigues a Barbería El Parque", Rating: 4.5}
	cafe := MarketCandidate{Offer: mkOffer("2x1 capuchino", "Café Sierra", "Cafeterías", 40*time.Minute, 8000, 16000), DistanceM: 900, Trend: 1, Rating: 4.8}
	bar := MarketCandidate{Offer: mkOffer("Cóctel a mitad", "Bar Muralla", "Bares", 3*time.Hour, 12000, 24000), DistanceM: 1100, Trend: 3}
	pizza := MarketCandidate{Offer: mkOffer("Pizza mediana", "Pizzería Bahía", "Restaurantes", 4*time.Hour, 25000, 34000), DistanceM: 2400, Trend: 2.5}
	spa := MarketCandidate{Offer: mkOffer("Masaje 45 min", "Spa Mamatoco", "Belleza", 5*time.Hour, 60000, 80000), DistanceM: 2600, Trend: 2}
	far := MarketCandidate{Offer: mkOffer("Lejana", "Otro pueblo", "Restaurantes", time.Hour, 1000, 2000), DistanceM: 50000, Trend: 9}
	all := []MarketCandidate{alm, corte, cafe, bar, pizza, spa, far}

	s := BuildMarket(all, MarketOptions{HasLocation: true, Term: "almuerzo", Personal: true})
	// Ninguna oferta en dos secciones de lado.
	seen := map[uuid.UUID]string{}
	for name, list := range map[string][]MarketCandidate{"near": s.Near, "searched": s.Searched, "trending": s.Trending, "foryou": s.ForYou} {
		for _, c := range list {
			if prev, ok := seen[c.Offer.ID]; ok {
				t.Fatalf("%q está en %s y en %s", c.Offer.Title, prev, name)
			}
			seen[c.Offer.ID] = name
		}
	}
	// El término elegido se queda con su oferta aunque esté cerca.
	if len(s.Searched) != 1 || s.Searched[0].Offer.ID != alm.Offer.ID {
		t.Fatalf("buscado: %+v", s.Searched)
	}
	// Lo personal también va antes que «cerca».
	if len(s.ForYou) != 1 || s.ForYou[0].Offer.ID != corte.Offer.ID || s.TopRated || s.ForYou[0].Reason == "" {
		t.Fatalf("para ti: %+v", s.ForYou)
	}
	// Cerca: dentro de 3 km, de la más cercana; sin la lejana.
	if len(s.Near) == 0 || s.Near[0].Offer.ID != cafe.Offer.ID || marketIDs(s.Near)[far.Offer.ID] {
		t.Fatalf("cerca: %+v", s.Near)
	}
	// Nada de fuera de la zona (10 km) en ninguna parte, aunque sea la de más tendencia.
	if marketIDs(s.Trending)[far.Offer.ID] || marketIDs(s.All)[far.Offer.ID] {
		t.Fatal("la oferta a 50 km no debía salir")
	}
	// «Todas» es la zona completa, ordenada por distancia por defecto.
	if len(s.All) != 6 || s.All[0].Offer.ID != alm.Offer.ID {
		t.Fatalf("todas: %d, primera %q", len(s.All), s.All[0].Offer.Title)
	}
	// Orden por ahorro y por fin.
	if a := BuildMarket(all, MarketOptions{HasLocation: true, Sort: "save"}).All; a[0].Offer.ID != cafe.Offer.ID && a[0].Offer.ID != bar.Offer.ID {
		t.Fatalf("mayor ahorro primero: %q", a[0].Offer.Title)
	}
	if a := BuildMarket(all, MarketOptions{HasLocation: true, Sort: "ends"}).All; a[0].Offer.ID != cafe.Offer.ID {
		t.Fatalf("termina antes primero: %q", a[0].Offer.Title)
	}
}

func TestBuildMarketEdges(t *testing.T) {
	a := MarketCandidate{Offer: mkOffer("A", "Uno", "X", time.Hour, 1, 2), DistanceM: -1, Rating: 4.1, Trend: 3}
	b := MarketCandidate{Offer: mkOffer("B", "Dos", "X", 2*time.Hour, 1, 2), DistanceM: -1, Rating: 4.9}
	c := MarketCandidate{Offer: mkOffer("C", "Tres", "X", 30*time.Minute, 1, 2), DistanceM: -1}
	// Sin ubicación: no hay «cerca», todo entra a la zona y el orden por defecto es el de fin.
	s := BuildMarket([]MarketCandidate{a, b, c}, MarketOptions{})
	if s.Near != nil || len(s.All) != 3 || s.All[0].Offer.ID != c.Offer.ID {
		t.Fatalf("sin ubicación: near=%v all0=%q", s.Near, s.All[0].Offer.Title)
	}
	// Sin sesión: las mejor valoradas, marcadas como tal y sin motivo personal.
	if !s.TopRated || len(s.ForYou) != 2 || s.ForYou[0].Offer.ID != b.Offer.ID || s.ForYou[0].Reason != "" {
		t.Fatalf("mejor valoradas: %+v", s.ForYou)
	}
	// Con una sola en tendencia, la sección no aparece (y la oferta sigue en «Todas»).
	if s.Trending != nil || !marketIDs(s.All)[a.Offer.ID] {
		t.Fatalf("tendencia con 1 debía esconderse: %+v", s.Trending)
	}
	// Nada: todo vacío, sin errores.
	if e := BuildMarket(nil, MarketOptions{HasLocation: true, Term: "x", Personal: true}); len(e.All) != 0 || e.Near != nil || len(e.Searched) != 0 {
		t.Fatalf("vacío: %+v", e)
	}
}
