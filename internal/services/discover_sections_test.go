package services

import (
	"fmt"
	"testing"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func cand(name string, dist float64, open bool, offerIn, postAgo time.Duration, now time.Time) SectionCandidate {
	c := SectionCandidate{Entity: models.Entity{ID: uuid.New(), Name: name}, DistanceMeters: dist, IsOpenNow: open}
	if offerIn != 0 {
		c.Offer = &models.Offer{Title: "Oferta de " + name, EndsAt: now.Add(offerIn)}
	}
	if postAgo != 0 {
		t := now.Add(-postAgo)
		c.LatestPost = &t
	}
	return c
}

func ids(sec Section) []string {
	out := make([]string, len(sec.Items))
	for i, it := range sec.Items {
		out[i] = it.Candidate.Entity.Name
	}
	return out
}

// Cada sección elige y ordena por su propia regla, con una razón por tarjeta.
func TestBuildSections_RulesAndOrder(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	cands := []SectionCandidate{
		cand("A", 300, true, 25*time.Minute, time.Hour, now),
		cand("B", 100, true, 5*time.Hour, 0, now),
		cand("C", 500, false, 2*time.Hour, 3*24*time.Hour, now),
		cand("D", 200, true, 0, 2*time.Hour, now),
		cand("E", 50, false, 0, 10*24*time.Hour, now), // publicación vieja: no es «nuevo»
	}
	secs := BuildSections(cands, now)
	got := map[string][]string{}
	for _, s := range secs {
		got[s.ID] = ids(s)
	}
	if fmt.Sprint(got[SectionOffers]) != "[A C B]" {
		t.Errorf("ofertas = %v, se esperaba [A C B] (la que termina antes primero)", got[SectionOffers])
	}
	if fmt.Sprint(got[SectionOpen]) != "[B D A]" {
		t.Errorf("abiertos = %v, se esperaba [B D A] (del más cercano al más lejano)", got[SectionOpen])
	}
	if fmt.Sprint(got[SectionNew]) != "[A D C]" {
		t.Errorf("nuevo = %v, se esperaba [A D C] (lo más reciente primero, sin lo de hace 10 días)", got[SectionNew])
	}
	if len(secs) != 3 || secs[0].ID != SectionOffers || secs[1].ID != SectionOpen || secs[2].ID != SectionNew {
		t.Errorf("orden de secciones incorrecto: %+v", secs)
	}
	first := secs[0].Items[0].Reason
	if first.Kind != SectionOffers || first.EndsAt == nil || first.OfferTitle != "Oferta de A" {
		t.Errorf("razón de oferta = %+v", first)
	}
	if r := secs[2].Items[0].Reason; r.Kind != SectionNew || r.PostedAt == nil {
		t.Errorf("razón de nuevo = %+v", r)
	}
}

// Una sección con menos de 3 negocios no se envía; ninguna tiene más de 6.
func TestBuildSections_MinAndLimit(t *testing.T) {
	now := time.Now()
	var cands []SectionCandidate
	for i := 0; i < 9; i++ {
		cands = append(cands, cand(fmt.Sprintf("N%d", i), float64(i), true, 0, 0, now))
	}
	cands = append(cands, cand("O1", 1, false, time.Hour, 0, now), cand("O2", 2, false, 2*time.Hour, 0, now))
	secs := BuildSections(cands, now)
	if len(secs) != 1 || secs[0].ID != SectionOpen {
		t.Fatalf("secciones = %+v, se esperaba solo «abiertos» (ofertas tiene 2 y nuevo 0)", secs)
	}
	if len(secs[0].Items) != SectionLimit {
		t.Errorf("abiertos trae %d, el máximo es %d", len(secs[0].Items), SectionLimit)
	}
	if got := BuildSections(nil, now); len(got) != 0 {
		t.Errorf("sin negocios debía devolver nada, devolvió %v", got)
	}
}

// La portada es el banner; si no hay, la primera foto que no sea video.
func TestCoverURL(t *testing.T) {
	e := models.Entity{Photos: []models.EntityPhoto{
		{Order: 2, Media: models.Media{URL: "b.jpg", ContentType: "image/jpeg"}},
		{Order: 0, Media: models.Media{URL: "v.mp4", ContentType: "video/mp4"}},
		{Order: 1, Media: models.Media{URL: "a.jpg", ContentType: "image/jpeg"}},
	}}
	if got := CoverURL(&e); got != "a.jpg" {
		t.Errorf("sin banner = %q, se esperaba a.jpg", got)
	}
	e.BannerURL = "banner.jpg"
	if got := CoverURL(&e); got != "banner.jpg" {
		t.Errorf("con banner = %q", got)
	}
	if got := CoverURL(&models.Entity{}); got != "" {
		t.Errorf("sin fotos = %q", got)
	}
}

// Integración: el servicio junta negocios del área, ofertas vigentes, horario y
// última publicación, y respeta la categoría.
func TestSections_FromDatabase(t *testing.T) {
	db := openTestDB(t, true)
	owner := newUser(t, db)
	now := utils.NowInCartagena()
	cat := models.Category{Name: "Barberías"}
	other := models.Category{Name: "Cafeterías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}

	mk := func(name string, category uuid.UUID, lat float64, open bool, tagline string) models.Entity {
		e := models.Entity{OwnerID: owner, Name: name, CategoryID: category, Latitude: lat, Longitude: -75.5, Tagline: tagline}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatalf("crear %s: %v", name, err)
		}
		if open {
			for d := 0; d < 7; d++ {
				if err := db.Create(&models.BusinessHour{EntityID: e.ID, Weekday: d, Is24h: true}).Error; err != nil {
					t.Fatal(err)
				}
			}
		}
		return e
	}
	offer := func(e models.Entity, in time.Duration) {
		o := models.Offer{EntityID: e.ID, Type: models.OfferHours, Title: "Oferta " + e.Name, Price: "1", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(in), TotalSlots: 5, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
		if err := db.Omit(clause.Associations).Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}
	post := func(e models.Entity, ago time.Duration) {
		if err := db.Omit(clause.Associations).Create(&models.Post{EntityID: e.ID, CreatedAt: now.Add(-ago)}).Error; err != nil {
			t.Fatal(err)
		}
	}

	a := mk("A", cat.ID, 10.401, true, "Corte clásico y barba")
	b := mk("B", cat.ID, 10.402, true, "")
	c := mk("C", cat.ID, 10.403, true, "")
	d := mk("D", cat.ID, 10.404, true, "")
	e := mk("E", cat.ID, 10.405, false, "")
	f := mk("F", other.ID, 10.4005, true, "")
	mk("Lejos", cat.ID, 11.5, true, "")
	offer(a, 30*time.Minute)
	offer(b, 2*time.Hour)
	offer(c, 5*time.Hour)
	offer(f, 10*time.Minute)
	post(a, time.Hour)
	post(b, 3*24*time.Hour)
	post(d, 2*time.Hour)
	post(e, 10*24*time.Hour)

	ranking := &RankingService{EntityService: &EntityService{Repo: repository.NewEntityRepository(db)}, OfferRepo: repository.NewOfferRepository(db)}
	secs, err := ranking.Sections(10.3, 10.5, -75.6, -75.4, 10.4, -75.5, cat.ID.String(), "")
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	got := map[string][]string{}
	var first SectionItem
	for _, s := range secs {
		got[s.ID] = ids(s)
		if s.ID == SectionOpen {
			first = s.Items[0]
		}
	}
	if fmt.Sprint(got[SectionOffers]) != "[A B C]" {
		t.Errorf("ofertas = %v, se esperaba [A B C] (F es de otra categoría)", got[SectionOffers])
	}
	if fmt.Sprint(got[SectionOpen]) != "[A B C D]" {
		t.Errorf("abiertos = %v, se esperaba [A B C D] (E está cerrado, «Lejos» fuera del área)", got[SectionOpen])
	}
	if fmt.Sprint(got[SectionNew]) != "[A D B]" {
		t.Errorf("nuevo = %v, se esperaba [A D B] (lo de hace 10 días no cuenta)", got[SectionNew])
	}
	if first.Candidate.ClosesAt != "24h" || first.Candidate.Entity.Tagline != "Corte clásico y barba" {
		t.Errorf("A: cierra = %q, frase = %q", first.Candidate.ClosesAt, first.Candidate.Entity.Tagline)
	}

	// Sin categoría entra F, que tiene la oferta que termina antes.
	all, _ := ranking.Sections(10.3, 10.5, -75.6, -75.4, 10.4, -75.5, "", "")
	if len(all) == 0 || all[0].ID != SectionOffers || all[0].Items[0].Candidate.Entity.Name != "F" {
		t.Errorf("sin categoría la primera oferta debía ser F: %+v", all)
	}
}

// «Ver más» pide más negocios por sección con el mismo criterio.
func TestBuildSectionsN_LargerLimit(t *testing.T) {
	now := time.Now()
	var cands []SectionCandidate
	for i := 0; i < 9; i++ {
		cands = append(cands, cand(fmt.Sprintf("N%d", i), float64(i), true, 0, 0, now))
	}
	secs := BuildSectionsN(cands, now, 50)
	if len(secs) != 1 || len(secs[0].Items) != 9 || secs[0].Items[0].Candidate.Entity.Name != "N0" {
		t.Errorf("con tope 50 debían venir los 9 abiertos en orden, vino %+v", secs)
	}
}
