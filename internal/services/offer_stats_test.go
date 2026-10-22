package services

import (
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func intPtr(n int) *int { return &n }

// st arma una oferta terminada «por horas» para las pruebas puras.
func st(hour, durMin int, views, claims, red int64, filled *int) OfferStat {
	return OfferStat{OfferID: uuid.New(), Type: models.OfferHours, Ended: true, Hour: hour, DurationMinutes: durMin, Views: views, Claims: claims, Redeemed: red, FilledMinutes: filled, StartsAt: time.Now()}
}

func TestOfferSummary_NotEnoughData(t *testing.T) {
	// Dos terminadas: no alcanza aunque tengan muchas vistas.
	s := BuildOfferSummary([]OfferStat{st(16, 120, 100, 10, 9, intPtr(60)), st(9, 120, 100, 4, 3, nil)})
	if s.Enough || len(s.Insights) != 0 || s.BestOfferID != nil || s.PreferBand != "" {
		t.Fatalf("sin datos suficientes no se sugiere nada: %+v", s)
	}
	// Tres terminadas pero pocas vistas.
	s = BuildOfferSummary([]OfferStat{st(16, 120, 5, 1, 1, nil), st(9, 120, 5, 1, 0, nil), st(10, 120, 5, 0, 0, nil)})
	if s.Enough {
		t.Fatal("con menos de 20 vistas no alcanza")
	}
	// Las activas no cuentan como terminadas.
	active := st(16, 120, 500, 10, 9, nil)
	active.Ended = false
	s = BuildOfferSummary([]OfferStat{active, st(16, 120, 10, 1, 1, nil), st(9, 120, 10, 1, 1, nil)})
	if s.Finished != 2 || s.Enough || s.Views != 20 {
		t.Fatalf("las activas no entran al resumen: %+v", s)
	}
	if empty := BuildOfferSummary(nil); empty.Enough || empty.Insights == nil {
		t.Fatalf("sin ofertas: resumen vacío pero con lista de hallazgos (no nil): %+v", empty)
	}
}

func TestOfferSummary_Insights(t *testing.T) {
	stats := []OfferStat{
		st(16, 120, 120, 10, 9, intPtr(70)), // tarde, corta, se agotó
		st(9, 120, 60, 4, 3, nil),           // mañana, corta
		st(18, 180, 140, 10, 8, intPtr(95)), // tarde, larga, se agotó
		st(10, 240, 35, 2, 1, nil),          // mañana, larga
		st(17, 120, 90, 8, 7, intPtr(50)),   // tarde, corta, se agotó
		st(15, 240, 110, 9, 6, nil),         // tarde, larga
	}
	// Una de todo el día con muchos canjes: no cuenta para franja ni duración.
	day := st(8, 720, 210, 14, 12, nil)
	day.Type = models.OfferDay
	stats = append(stats, day)

	s := BuildOfferSummary(stats)
	if !s.Enough || s.Finished != 7 || s.Redeemed != 46 {
		t.Fatalf("resumen: %+v", s)
	}
	if s.BestOfferID == nil || *s.BestOfferID != day.OfferID {
		t.Fatal("la mejor es la de más canjes (aunque sea de todo el día)")
	}
	kinds := map[string]string{}
	for _, i := range s.Insights {
		kinds[i.Kind] = i.Text
	}
	// Franja: tarde (9+8+7+6)/4 = 7,5 contra mañana (3+1)/2 = 2 → 3,8 veces.
	if s.PreferBand != "tarde" || !strings.Contains(kinds["band"], "tarde tuvieron 3,8 veces más canjes que las de la mañana") {
		t.Fatalf("franja: %q / %q", s.PreferBand, kinds["band"])
	}
	// Duración: cortas 2 de 3 agotadas; largas 1 de 3.
	if !s.PreferShort || !strings.Contains(kinds["duration"], "2 de 3 veces; las más largas, 1 de 3") {
		t.Fatalf("duración: %v %q", s.PreferShort, kinds["duration"])
	}
	// Promedio de las que se agotaron: (70+95+50)/3 = 71.
	if s.AvgFillMinutes == nil || *s.AvgFillMinutes != 71 || !strings.Contains(kinds["fill"], "unos 71 min") {
		t.Fatalf("agotadas: %v %q", s.AvgFillMinutes, kinds["fill"])
	}
	for _, i := range s.Insights {
		if strings.Contains(strings.ToLower(i.Text), "garantiza") || strings.Contains(i.Text, "siempre") {
			t.Fatalf("no se promete un efecto: %q", i.Text)
		}
	}
}

func TestOfferSummary_NoClearPattern(t *testing.T) {
	// Franjas parecidas (menos de 1,5 veces) y una sola larga: ni franja ni duración.
	s := BuildOfferSummary([]OfferStat{st(16, 120, 50, 5, 4, nil), st(17, 120, 50, 5, 4, nil), st(9, 120, 50, 5, 3, nil), st(10, 180, 50, 5, 3, nil)})
	for _, i := range s.Insights {
		if i.Kind == "band" || i.Kind == "duration" {
			t.Fatalf("sin un patrón claro no se dice nada: %+v", s.Insights)
		}
	}
	if s.PreferBand != "" || s.PreferShort {
		t.Fatalf("sin preferencias: %+v", s)
	}
	// Nadie canjeó: no hay «mejor oferta».
	z := BuildOfferSummary([]OfferStat{st(16, 120, 50, 0, 0, nil), st(9, 120, 50, 0, 0, nil), st(10, 120, 50, 0, 0, nil)})
	if z.BestOfferID != nil {
		t.Fatal("sin canjes no hay mejor oferta")
	}
}

// Con la base de datos: vistas distintas por instalación, reclamos y canjes por oferta, minutos hasta agotarse, solo
// el dueño, y solo las ofertas de los últimos 90 días del negocio.
func TestOfferStats_FromDatabase(t *testing.T) {
	db := openTestDB(t, false)
	owner, stranger := newUser(t, db), newUser(t, db)
	cat := models.Category{Name: "Barberías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	mkEntity := func(o uuid.UUID) uuid.UUID {
		e := models.Entity{OwnerID: o, Name: "La Navaja", CategoryID: cat.ID, Latitude: 11.2, Longitude: -74.2}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e.ID
	}
	shop, other := mkEntity(owner), mkEntity(stranger)
	now := time.Now()
	mkOffer := func(entity uuid.UUID, title string, starts time.Time, dur time.Duration, slots int) models.Offer {
		o := models.Offer{EntityID: entity, Type: models.OfferHours, Title: title, Price: "1", StartsAt: starts, EndsAt: starts.Add(dur), TotalSlots: slots, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
		if err := db.Omit(clause.Associations).Create(&o).Error; err != nil {
			t.Fatal(err)
		}
		return o
	}
	full := mkOffer(shop, "Se agotó", now.Add(-48*time.Hour), 2*time.Hour, 3)
	half := mkOffer(shop, "A medias", now.Add(-24*time.Hour), 2*time.Hour, 10)
	mkOffer(shop, "Activa", now.Add(-30*time.Minute), 2*time.Hour, 5)
	mkOffer(shop, "Muy vieja", now.Add(-120*24*time.Hour), 2*time.Hour, 5)
	mkOffer(other, "De otro negocio", now.Add(-24*time.Hour), 2*time.Hour, 5)

	claim := func(o models.Offer, at time.Time, status models.ClaimStatus) {
		c := models.OfferClaim{OfferID: o.ID, UserID: newUser(t, db), EntityID: o.EntityID, Status: status, Token: uuid.NewString(), ShortCode: uuid.NewString()[:6], ClaimedAt: at, ExpiresAt: at.Add(30 * time.Minute)}
		if err := db.Omit(clause.Associations).Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	claim(full, full.StartsAt.Add(10*time.Minute), models.ClaimRedeemed)
	claim(full, full.StartsAt.Add(20*time.Minute), models.ClaimRedeemed)
	claim(full, full.StartsAt.Add(45*time.Minute), models.ClaimExpired) // el tercer cupo: se agotó a los 45 min
	claim(half, half.StartsAt.Add(5*time.Minute), models.ClaimRedeemed)

	view := func(o models.Offer, anon string) {
		e := models.Event{Name: "offer_viewed", AnonID: anon, EntityID: &o.EntityID, Props: map[string]any{"offer_id": o.ID.String()}, CreatedAt: now}
		if err := db.Create(&e).Error; err != nil {
			t.Fatal(err)
		}
	}
	view(full, "a")
	view(full, "a") // la misma instalación cuenta una vez
	view(full, "b")
	view(half, "c")
	noise := models.Event{Name: "profile_opened", AnonID: "d", EntityID: &full.EntityID, Props: map[string]any{"origin": "map"}, CreatedAt: now}
	if err := db.Create(&noise).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewOfferService(repository.NewOfferRepository(db), repository.NewEntityRepository(db))
	if _, _, err := svc.Stats(shop, stranger, now); err == nil {
		t.Fatal("solo el dueño ve las estadísticas")
	}
	stats, sum, err := svc.Stats(shop, owner, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 3 {
		t.Fatalf("3 ofertas de los últimos 90 días del negocio (sin la vieja ni la ajena): %d", len(stats))
	}
	by := map[string]OfferStat{}
	for _, s := range stats {
		by[s.Title] = s
	}
	f := by["Se agotó"]
	if f.Views != 2 || f.Claims != 3 || f.Redeemed != 2 || f.FilledMinutes == nil || *f.FilledMinutes != 45 || !f.Ended || f.DurationMinutes != 120 {
		t.Fatalf("se agotó: %+v (minutos %v)", f, f.FilledMinutes)
	}
	h := by["A medias"]
	if h.Views != 1 || h.Claims != 1 || h.Redeemed != 1 || h.FilledMinutes != nil || !h.Ended {
		t.Fatalf("a medias: %+v", h)
	}
	if by["Activa"].Ended || stats[0].Title != "Activa" {
		t.Fatalf("la activa no está terminada y va primero (más reciente): %+v", stats[0])
	}
	if sum.Finished != 2 || sum.Enough || sum.Redeemed != 3 {
		t.Fatalf("resumen (dos terminadas: sin datos suficientes): %+v", sum)
	}
}
