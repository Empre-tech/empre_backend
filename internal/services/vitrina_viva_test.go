package services

import (
	"errors"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// 3.40: «Mayor ahorro de hoy» y «Haz que tu negocio destaque».

func TestPickFeatured(t *testing.T) {
	// 10:00 en Bogotá; «hoy» termina a medianoche de Bogotá (05:00 UTC del día siguiente).
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, marketTZ)
	mk := func(title string, ends time.Time, sp, rp int) MarketCandidate {
		o := mkOffer(title, title, "X", 0, sp, rp)
		o.EndsAt = ends
		return MarketCandidate{Offer: o}
	}
	half := mk("Mitad, termina 22:00", time.Date(2026, 10, 19, 22, 0, 0, 0, marketTZ), 5000, 10000)
	third := mk("Tercio, termina 12:00", time.Date(2026, 10, 19, 12, 0, 0, 0, marketTZ), 2000, 3000)
	tomorrow := mk("80 % mañana", time.Date(2026, 10, 20, 0, 30, 0, 0, marketTZ), 2000, 10000)
	ended := mk("90 % ya terminó", now.Add(-time.Minute), 1000, 10000)
	if f := PickFeatured([]MarketCandidate{third, tomorrow, half, ended}, now); f == nil || f.Offer.ID != half.Offer.ID {
		t.Fatalf("debía ganar la de mayor ahorro que termina hoy (no la de mañana ni la terminada): %v", f)
	}
	// Empate de ahorro: la que termina antes.
	early := mk("Mitad, termina 11:00", time.Date(2026, 10, 19, 11, 0, 0, 0, marketTZ), 5000, 10000)
	if f := PickFeatured([]MarketCandidate{half, early}, now); f.Offer.ID != early.Offer.ID {
		t.Fatalf("empate: %s", f.Offer.Title)
	}
	// Sin ahorro real (sin precio de referencia) no hay destacada.
	plain := MarketCandidate{Offer: models.Offer{ID: uuid.New(), Title: "Sin precio", EndsAt: now.Add(time.Hour)}}
	if f := PickFeatured([]MarketCandidate{plain, tomorrow}, now); f != nil {
		t.Fatalf("sin ahorro ni hoy: %s", f.Offer.Title)
	}
	// La destacada no se repite: WithoutOffer la quita.
	if rest := WithoutOffer([]MarketCandidate{half, third}, half.Offer.ID); len(rest) != 1 || rest[0].Offer.ID != third.Offer.ID {
		t.Fatalf("sin la destacada: %d", len(rest))
	}
}

func TestBuildCompleteness(t *testing.T) {
	empty := BuildCompleteness(CompletenessInput{Tagline: "  ", Description: "corta"})
	keys := ""
	for _, i := range empty {
		keys += i.Key + ","
		if i.Done {
			t.Fatalf("%s no debía estar hecho", i.Key)
		}
	}
	if keys != "photo,cover,photos,hours,tagline,description,catalog,offer," {
		t.Fatalf("orden: %s", keys)
	}
	full := BuildCompleteness(CompletenessInput{HasProfile: true, HasCover: true, Photos: 3, HasHours: true, Tagline: "Cortes clásicos",
		Description: "Barbería de barrio con cortes clásicos y degradados desde 2010.", PricedItems: 1, OffersEver: 1})
	for _, i := range full {
		if !i.Done {
			t.Fatalf("%s debía estar hecho", i.Key)
		}
	}
	if BuildCompleteness(CompletenessInput{Photos: 2})[2].Done {
		t.Fatal("2 fotos no son 3")
	}
	groups := map[string]string{}
	for _, i := range full {
		groups[i.Key] = i.Group
	}
	if groups["photo"] != "look" || groups["hours"] != "found" || groups["offer"] != "sell" {
		t.Fatalf("grupos: %v", groups)
	}
}

func TestCompletenessIntegration(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.Offer{}, &models.OfferItem{}, &models.Post{}, &models.EntityPhoto{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Name: "Dueña", Email: "duena@x.co", PasswordHash: "x"}
	other := models.User{Name: "Otro", Email: "otro@x.co", PasswordHash: "x"}
	db.Create(&owner)
	db.Create(&other)
	cat := models.Category{Name: "Belleza"}
	db.Create(&cat)
	e := models.Entity{OwnerID: owner.ID, Name: "Spa", CategoryID: cat.ID, Tagline: "Masajes", Latitude: 11.24, Longitude: -74.2}
	if err := db.Omit("Owner", "Category").Create(&e).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewCompletenessService(db)
	if _, err := svc.For(e.ID, other.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("otro usuario: %v", err)
	}
	done := func() map[string]bool {
		items, err := svc.For(e.ID, owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, i := range items {
			m[i.Key] = i.Done
		}
		return m
	}
	if d := done(); !d["tagline"] || d["hours"] || d["catalog"] || d["offer"] {
		t.Fatalf("al empezar: %v", d)
	}
	// Horario, un servicio con precio (uno sin precio no cuenta) y una oferta que luego se borra: siguen hechos.
	db.Create(&models.BusinessHour{EntityID: e.ID, Weekday: 1, OpenTime: "09:00", CloseTime: "18:00"})
	p := 50000
	db.Create(&models.ServiceItem{EntityID: e.ID, Name: "Sin precio", Status: models.ServiceActive})
	if d := done(); d["catalog"] {
		t.Fatal("un servicio sin precio no cuenta")
	}
	db.Create(&models.ServiceItem{EntityID: e.ID, Name: "Masaje", Price: &p, Status: models.ServiceActive})
	o := models.Offer{EntityID: e.ID, Type: models.OfferHours, Title: "Masaje", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), TotalSlots: 5}
	if err := db.Omit("Entity", "Items").Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	db.Delete(&o)
	if d := done(); !d["hours"] || !d["catalog"] || !d["offer"] {
		t.Fatalf("después: %v (la oferta borrada cuenta como publicada)", d)
	}
	if _, err := svc.For(uuid.New(), owner.ID); err == nil {
		t.Fatal("negocio inexistente")
	}
}
