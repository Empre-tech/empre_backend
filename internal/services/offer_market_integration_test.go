package services

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

// Vitrina de ofertas (3.38) con PostgreSQL: búsquedas reales con mínimo, tendencia por reclamos y vistas, recomendado
// por seguidos y reclamos propios, valoración sin sesión, zona y sin ofertas repetidas.
func TestOfferMarketIntegration(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.Offer{}, &models.OfferItem{}, &models.OfferClaim{}, &models.Event{}, &models.Favorite{}, &models.Review{}, &models.Subcategory{}, &models.Zone{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mkUser := func(n string) models.User {
		u := models.User{Name: n, Email: n + "@x.co", PasswordHash: "x"}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}
	owner, viewer, other := mkUser("duena"), mkUser("ana"), mkUser("beto")
	food := models.Category{Name: "Restaurantes"}
	beauty := models.Category{Name: "Belleza"}
	db.Create(&food)
	db.Create(&beauty)
	// Centro de referencia 11.2400, -74.2000; 0.001° ≈ 111 m. Una zona «Centro» de 1 km en la ciudad de prueba (3.39).
	centro := models.Zone{City: "Santa Marta", Name: "Centro", Latitude: 11.24, Longitude: -74.2, RadiusM: 1000, Active: true}
	db.Create(&centro)
	mkEnt := func(name string, cat models.Category, dLat float64) models.Entity {
		city := "santa marta" // escrita distinto a la zona: igual cuenta como la misma ciudad
		if dLat > 0.1 {
			city = "Otro pueblo"
		}
		e := models.Entity{OwnerID: owner.ID, Name: name, CategoryID: cat.ID, Latitude: 11.24 + dLat, Longitude: -74.2, City: city}
		if dLat < 0.009 {
			e.ZoneID = &centro.ID
		}
		if err := db.Omit("Owner", "Category").Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e
	}
	sazon := mkEnt("La Sazón", food, 0.003)              // ~330 m
	barber := mkEnt("Barbería El Parque", beauty, 0.006) // ~670 m
	bahia := mkEnt("Pizzería Bahía", food, 0.02)         // ~2.2 km
	spa := mkEnt("Spa Mamatoco", beauty, 0.024)          // ~2.7 km
	lejos := mkEnt("Otro pueblo", food, 0.5)             // ~55 km
	mkOffer := func(e models.Entity, title string, ends time.Duration) models.Offer {
		// Sin precio de referencia (sin ahorro): así no hay «Mayor ahorro de hoy» que dependa de la hora del día.
		sp := 1000
		o := models.Offer{EntityID: e.ID, Type: models.OfferHours, Title: title, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(ends), TotalSlots: 10, SpecialPrice: &sp}
		if err := db.Omit("Entity", "Items").Create(&o).Error; err != nil {
			t.Fatal(err)
		}
		return o
	}
	alm := mkOffer(sazon, "Almuerzo ejecutivo + jugo", 3*time.Hour)
	corte := mkOffer(barber, "Corte + barba", 6*time.Hour)
	pizza := mkOffer(bahia, "Pizza mediana", 2*time.Hour)
	masaje := mkOffer(spa, "Masaje 45 min", 5*time.Hour)
	mkOffer(lejos, "Almuerzo lejano", time.Hour)
	vencida := models.Offer{EntityID: sazon.ID, Type: models.OfferHours, Title: "Vencida", StartsAt: now.Add(-3 * time.Hour), EndsAt: now.Add(-time.Minute), TotalSlots: 5}
	db.Omit("Entity", "Items").Create(&vencida)

	// Búsquedas de la semana: «almuerzo» 6 veces (pasa el mínimo), «pizza» 3 (no pasa), una vieja no cuenta.
	ev := func(name string, props map[string]any, at time.Time) {
		e := models.Event{Name: name, AnonID: "anon-" + uuid.NewString()[:8], Props: props, CreatedAt: at}
		if err := db.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 6; i++ {
		ev("search_submitted", map[string]any{"query": "almuerzo"}, now.Add(-time.Duration(i)*time.Hour))
	}
	for i := 0; i < 3; i++ {
		ev("search_submitted", map[string]any{"query": "pizza"}, now)
	}
	for i := 0; i < 9; i++ {
		ev("search_submitted", map[string]any{"query": "viejo"}, now.Add(-10*24*time.Hour))
	}
	// Tendencia: pizza con 2 reclamos recientes; masaje con 8 vistas (=2); corte con un reclamo viejo (no cuenta).
	claim := func(o models.Offer, u models.User, at time.Time) {
		c := models.OfferClaim{OfferID: o.ID, UserID: u.ID, EntityID: o.EntityID, Token: uuid.NewString(), ShortCode: uuid.NewString()[:6], ClaimedAt: at, ExpiresAt: at.Add(time.Hour)}
		if err := db.Omit("Offer", "User", "Entity").Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	claim(pizza, other, now.Add(-time.Hour))
	claim(pizza, owner, now.Add(-2*time.Hour))
	claim(corte, other, now.Add(-5*24*time.Hour))
	for i := 0; i < 8; i++ {
		ev("offer_viewed", map[string]any{"offer_id": masaje.ID.String()}, now.Add(-time.Hour))
	}
	// Ana sigue a la barbería; Beto dejó una reseña de 5 en el spa.
	db.Create(&models.Favorite{UserID: viewer.ID, EntityID: barber.ID})
	db.Omit("User").Create(&models.Review{EntityID: spa.ID, UserID: other.ID, Rating: 5})

	svc := NewOfferMarketService(db, nil)
	res, err := svc.Market(&viewer.ID, 11.24, -74.2, true, "", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	s := res.Sections
	titles := func(l []MarketCandidate) string {
		out := []string{}
		for _, c := range l {
			out = append(out, c.Offer.Title)
		}
		return strings.Join(out, ", ")
	}
	t.Logf("ciudad %q · zonas %d · términos %v · buscado [%s] · para ti [%s] · cerca [%s] · tendencia [%s]",
		res.City, len(res.Zones), res.Terms, titles(s.Searched), titles(s.ForYou), titles(s.Near), titles(s.Trending))
	if res.Featured != nil {
		t.Fatalf("sin ahorro no hay destacada: %s", res.Featured.Offer.Title)
	}
	if res.City != "Santa Marta" || res.Here == nil || res.Here.ID != centro.ID {
		t.Fatalf("ciudad por la zona que contiene la ubicación: %q %v", res.City, res.Here)
	}
	if len(res.Zones) != 1 || !res.Zones[0].Here || len(res.Zones[0].Looks) != 2 {
		t.Fatalf("por zona: %+v", res.Zones)
	}

	if len(res.Terms) != 1 || res.Terms[0].Term != "almuerzo" || res.Terms[0].Label != "Almuerzo" || res.Term != "almuerzo" {
		t.Fatalf("términos: %+v (pizza no pasa el mínimo y «viejo» es de hace 10 días)", res.Terms)
	}
	if len(s.Searched) != 1 || s.Searched[0].Offer.ID != alm.ID {
		t.Fatalf("buscado: %s (la lejana no entra)", titles(s.Searched))
	}
	if len(s.ForYou) != 1 || s.ForYou[0].Offer.ID != corte.ID || s.ForYou[0].Reason != "Sigues a Barbería El Parque" {
		t.Fatalf("para ti: %+v", s.ForYou)
	}
	if titles(s.Near) != "Pizza mediana, Masaje 45 min" {
		t.Fatalf("cerca (3 km, sin repetir): %s", titles(s.Near))
	}
	if s.Trending != nil {
		t.Fatalf("tendencia: pizza y masaje ya salieron cerca, debía esconderse: %s", titles(s.Trending))
	}
	all, err := svc.MarketAll(11.24, -74.2, true, "", "", nil, "", "near", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all.Items {
		if c.Offer.EntityID == lejos.ID || c.Offer.ID == vencida.ID {
			t.Fatalf("en «todas» no va la de otra ciudad ni la vencida: %s", c.Offer.Title)
		}
	}
	if titles(all.Items) != "Almuerzo ejecutivo + jugo, Corte + barba, Pizza mediana, Masaje 45 min" || all.Next != -1 || all.City != "Santa Marta" {
		t.Fatalf("todas (ciudad entera, la más cercana primero): %s next=%d", titles(all.Items), all.Next)
	}
	// Por zona: solo las del Centro; búsqueda en el servidor.
	if z, _ := svc.MarketAll(11.24, -74.2, true, "", "", &centro.ID, "", "ends", 0, now); titles(z.Items) != "Almuerzo ejecutivo + jugo, Corte + barba" {
		t.Fatalf("zona Centro: %s", titles(z.Items))
	}
	if q, _ := svc.MarketAll(11.24, -74.2, true, "", "", nil, "pizza", "near", 0, now); titles(q.Items) != "Pizza mediana" {
		t.Fatalf("búsqueda: %s", titles(q.Items))
	}
	// Sin ubicación y con la última ciudad conocida: esa ciudad; sin ninguna, todas (también la de otro pueblo).
	if h, _ := svc.MarketAll(0, 0, false, "Santa Marta", "", nil, "", "near", 0, now); len(h.Items) != 4 || h.City != "Santa Marta" {
		t.Fatalf("última ciudad: %s", titles(h.Items))
	}
	if h, _ := svc.MarketAll(0, 0, false, "", "", nil, "", "near", 0, now); len(h.Items) != 5 || h.City != "" {
		t.Fatalf("sin ciudad: %s", titles(h.Items))
	}
	// Desde otro pueblo, la vitrina es la de allá.
	if o, _ := svc.Market(nil, 11.74, -74.2, true, "", "", "", now); o.City != "Otro pueblo" || o.Here != nil || len(o.Zones) != 0 {
		t.Fatalf("otro pueblo: %q", o.City)
	}

	// Sin ubicación y sin sesión: sin «cerca», tendencia con pizza y masaje, y «mejor valoradas» si hay reseñas.
	svc2 := NewOfferMarketService(db, nil)
	res2, err := svc2.Market(nil, 0, 0, false, "Santa Marta", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Sections.Near != nil || !res2.Sections.TopRated {
		t.Fatalf("sin ubicación/sesión: near=%v topRated=%v", res2.Sections.Near, res2.Sections.TopRated)
	}
	tr := titles(res2.Sections.Trending)
	if !strings.Contains(tr, "Pizza mediana") || !strings.Contains(tr, "Masaje 45 min") || strings.Contains(tr, "Corte") {
		t.Fatalf("tendencia (reclamos y vistas de 48 h): %s", tr)
	}
	// Sin sesión y con reseñas: «mejor valoradas» toma el spa (5 estrellas) antes que la tendencia.
	svc3 := NewOfferMarketService(db, NewReviewService(repository.NewReviewRepository(db), nil))
	res4, err := svc3.Market(nil, 11.24, -74.2, true, "", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !res4.Sections.TopRated || len(res4.Sections.ForYou) == 0 || res4.Sections.ForYou[0].Offer.ID != masaje.ID || res4.Sections.ForYou[0].Reason != "" {
		t.Fatalf("mejor valoradas: %s", titles(res4.Sections.ForYou))
	}
	// Filtro de categoría: solo Belleza.
	res3, _ := svc.MarketAll(11.24, -74.2, true, "", beauty.ID.String(), nil, "", "near", 0, now)
	for _, c := range res3.Items {
		if c.Offer.Entity.CategoryID != beauty.ID {
			t.Fatalf("filtro de categoría: %s", c.Offer.Title)
		}
	}

	// 3.40: una oferta con ahorro que termina hoy es la destacada y no se repite en ninguna sección.
	y, m, d := now.In(marketTZ).Date()
	mid := now.Add(time.Date(y, m, d+1, 0, 0, 0, 0, marketTZ).Sub(now) / 2)
	sp, rf := 6000, 12000
	feat := models.Offer{EntityID: bahia.ID, Type: models.OfferHours, Title: "Pizza familiar a mitad", StartsAt: now.Add(-time.Hour), EndsAt: mid, TotalSlots: 5, SpecialPrice: &sp, ReferencePrice: &rf}
	if err := db.Omit("Entity", "Items").Create(&feat).Error; err != nil {
		t.Fatal(err)
	}
	r5, err := svc.Market(&viewer.ID, 11.24, -74.2, true, "", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if r5.Featured == nil || r5.Featured.Offer.ID != feat.ID {
		t.Fatalf("destacada: %v", r5.Featured)
	}
	for _, l := range [][]MarketCandidate{r5.Sections.Near, r5.Sections.Searched, r5.Sections.Trending, r5.Sections.ForYou} {
		for _, c := range l {
			if c.Offer.ID == feat.ID {
				t.Fatal("la destacada se repitió en una sección")
			}
		}
	}
	_ = fmt.Sprintf
}
