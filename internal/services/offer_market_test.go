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
	// La lista completa: todas, ordenada por distancia por defecto; por ahorro y por fin.
	if p, _ := PageAll(all, AllOptions{HasLocation: true, Sort: "near"}, 0, 50); len(p) != 7 || p[0].Offer.ID != alm.Offer.ID {
		t.Fatalf("todas: %d, primera %q", len(p), p[0].Offer.Title)
	}
	if p, _ := PageAll(all, AllOptions{HasLocation: true, Sort: "save"}, 0, 50); offerSaving(p[0].Offer) < offerSaving(p[len(p)-1].Offer) || p[0].Offer.ID != far.Offer.ID && p[0].Offer.ID != cafe.Offer.ID && p[0].Offer.ID != bar.Offer.ID {
		t.Fatalf("mayor ahorro primero: %q", p[0].Offer.Title)
	}
	if p, _ := PageAll(all, AllOptions{HasLocation: true, Sort: "ends"}, 0, 50); p[0].Offer.ID != cafe.Offer.ID {
		t.Fatalf("termina antes primero: %q", p[0].Offer.Title)
	}
}

func TestBuildMarketEdges(t *testing.T) {
	a := MarketCandidate{Offer: mkOffer("A", "Uno", "X", time.Hour, 1, 2), DistanceM: -1, Rating: 4.1, Trend: 3}
	b := MarketCandidate{Offer: mkOffer("B", "Dos", "X", 2*time.Hour, 1, 2), DistanceM: -1, Rating: 4.9}
	c := MarketCandidate{Offer: mkOffer("C", "Tres", "X", 30*time.Minute, 1, 2), DistanceM: -1}
	// Sin ubicación: no hay «cerca», todo entra a la zona y el orden por defecto es el de fin.
	s := BuildMarket([]MarketCandidate{a, b, c}, MarketOptions{})
	if p, _ := PageAll([]MarketCandidate{a, b, c}, AllOptions{Sort: "near"}, 0, 10); s.Near != nil || len(p) != 3 || p[0].Offer.ID != c.Offer.ID {
		t.Fatalf("sin ubicación: near=%v all0=%q", s.Near, p[0].Offer.Title)
	}
	// Sin sesión: las mejor valoradas, marcadas como tal y sin motivo personal.
	if !s.TopRated || len(s.ForYou) != 2 || s.ForYou[0].Offer.ID != b.Offer.ID || s.ForYou[0].Reason != "" {
		t.Fatalf("mejor valoradas: %+v", s.ForYou)
	}
	// Con una sola en tendencia, la sección no aparece (y la oferta sigue en «Todas»).
	if s.Trending != nil {
		t.Fatalf("tendencia con 1 debía esconderse: %+v", s.Trending)
	}
	// Nada: todo vacío, sin errores.
	if e := BuildMarket(nil, MarketOptions{HasLocation: true, Term: "x", Personal: true}); e.Near != nil || len(e.Searched) != 0 {
		t.Fatalf("vacío: %+v", e)
	}
}

// 3.39: la ciudad la pone la ubicación; sin ella, la última conocida; nunca una por defecto.
func TestResolveCity(t *testing.T) {
	centro := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "Centro Histórico", Latitude: 11.2408, Longitude: -74.2110, RadiusM: 1200, Active: true}
	rodadero := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "El Rodadero", Latitude: 11.2058, Longitude: -74.2271, RadiusM: 1500, Active: true}
	boca := models.Zone{ID: uuid.New(), City: "Cartagena", Name: "Bocagrande", Latitude: 10.3990, Longitude: -75.5550, RadiusM: 1500, Active: true}
	zones := []models.Zone{boca, centro, rodadero}
	points := []CityPoint{{City: "Barranquilla", Lat: 10.9639, Long: -74.7964}, {City: "santa marta", Lat: 11.25, Long: -74.20}}

	if c, h := ResolveCity(zones, points, 11.2410, -74.2112, true, "Cartagena"); c != "Santa Marta" || h == nil || h.ID != centro.ID {
		t.Fatalf("dentro del Centro: %q %v (la ubicación manda sobre la última ciudad)", c, h)
	}
	if c, h := ResolveCity(zones, points, 11.2250, -74.2000, true, ""); c != "Santa Marta" || h != nil {
		t.Fatalf("entre zonas: %q %v (ciudad de la zona más cercana, sin «estás en»)", c, h)
	}
	if c, _ := ResolveCity(zones, points, 10.9700, -74.8000, true, ""); c != "Barranquilla" {
		t.Fatalf("sin zonas cerca: %q (la del negocio más cercano)", c)
	}
	if c, _ := ResolveCity(zones, points, 4.7110, -74.0721, true, "Santa Marta"); c != "" {
		t.Fatalf("lejos de todo: %q (sin ciudad, no se adivina)", c)
	}
	if c, h := ResolveCity(zones, points, 0, 0, false, "  SANTA MARTA "); c != "Santa Marta" || h != nil {
		t.Fatalf("sin ubicación con última ciudad: %q", c)
	}
	if c, _ := ResolveCity(zones, points, 0, 0, false, "barranquilla"); c != "Barranquilla" {
		t.Fatalf("última ciudad sin zonas: %q", c)
	}
	if c, _ := ResolveCity(zones, points, 0, 0, false, ""); c != "" {
		t.Fatalf("sin nada: %q (ninguna ciudad por defecto)", c)
	}
	if c, _ := ResolveCity(zones, points, 0, 0, false, "Medellín"); c != "" {
		t.Fatalf("ciudad desconocida: %q", c)
	}
}

func TestCityZonesAndPages(t *testing.T) {
	centro := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "Centro Histórico", Latitude: 11.2408, Longitude: -74.2110, RadiusM: 1200, Active: true}
	taganga := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "Taganga", Latitude: 11.2686, Longitude: -74.1917, RadiusM: 1200, Active: true}
	rodadero := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "El Rodadero", Latitude: 11.2058, Longitude: -74.2271, RadiusM: 1500, Active: true}
	boca := models.Zone{ID: uuid.New(), City: "Cartagena", Name: "Bocagrande", Latitude: 10.3990, Longitude: -75.5550, RadiusM: 1500, Active: true}
	food, sea := models.Category{ID: uuid.New(), Name: "Comida", Color: "red"}, models.Category{ID: uuid.New(), Name: "Mar", Color: "blue"}
	mk := func(title, city string, z *models.Zone, cat models.Category, ends time.Duration, d float64) MarketCandidate {
		o := mkOffer(title, title+" negocio", cat.Name, ends, 1000, 2000)
		o.Entity.City, o.Entity.CategoryID, o.Entity.Category = city, cat.ID, cat
		if z != nil {
			id := z.ID
			o.Entity.ZoneID = &id
		}
		return MarketCandidate{Offer: o, DistanceM: d}
	}
	cands := []MarketCandidate{
		mk("Almuerzo", "Santa Marta", &centro, food, 2*time.Hour, 300),
		mk("Ceviche", "santa marta", &taganga, sea, time.Hour, 3900),
		mk("Mojarra", "Santa Marta", &taganga, food, 3*time.Hour, 4000),
		mk("Sin zona", "Santa Marta", nil, food, 4*time.Hour, 6000),
		mk("Arepa", "Cartagena", &boca, food, time.Hour, 200000),
	}
	inCity := InCity(cands, "Santa Marta")
	if len(inCity) != 4 || len(InCity(cands, "")) != 5 {
		t.Fatalf("en la ciudad: %d (sin mayúsculas importa; vacía = todas)", len(inCity))
	}
	zones := []models.Zone{boca, rodadero, taganga, centro}
	zs := BuildZones(zones, inCity, "Santa Marta", &centro, 11.2408, -74.2110, true)
	if len(zs) != 2 || zs[0].Zone.ID != centro.ID || !zs[0].Here || zs[1].Zone.ID != taganga.ID || len(zs[1].Looks) != 2 {
		t.Fatalf("zonas: %+v (sin El Rodadero, que no tiene ofertas; ni Bocagrande, de otra ciudad)", zs)
	}
	if zs := BuildZones(zones, inCity, "Santa Marta", nil, 0, 0, false); zs[0].Zone.Name != "Centro Histórico" || zs[0].DistanceM >= 0 || zs[0].Here {
		t.Fatalf("sin ubicación, alfabético y sin distancia: %+v", zs)
	}
	if len(BuildZones(zones, cands, "", nil, 0, 0, false)) != 0 {
		t.Fatal("sin ciudad no hay «Por zona»")
	}
	// Tandas: no repiten ni saltan, y al final next = -1.
	seen := map[uuid.UUID]bool{}
	off, n := 0, 0
	for off >= 0 {
		var p []MarketCandidate
		p, off = PageAll(inCity, AllOptions{HasLocation: true, Sort: "near"}, off, 3)
		for _, c := range p {
			if seen[c.Offer.ID] {
				t.Fatalf("repetida entre tandas: %s", c.Offer.Title)
			}
			seen[c.Offer.ID] = true
		}
		n++
	}
	if len(seen) != 4 || n != 2 {
		t.Fatalf("tandas: %d ofertas en %d tandas", len(seen), n)
	}
	tz := taganga.ID
	if p, next := PageAll(inCity, AllOptions{Zone: &tz, Sort: "near"}, 0, 20); len(p) != 2 || p[0].Offer.Title != "Ceviche" || next != -1 {
		t.Fatalf("zona Taganga sin ubicación (termina antes primero): %d", len(p))
	}
	if p, _ := PageAll(inCity, AllOptions{Query: "mojarra"}, 0, 20); len(p) != 1 {
		t.Fatalf("búsqueda: %d", len(p))
	}
	if p, next := PageAll(inCity, AllOptions{}, 99, 20); len(p) != 0 || next != -1 {
		t.Fatal("cursor pasado del final: vacío")
	}
}
