package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
)

// 3.41: el brillo de cada zona y los pulsos salen de datos reales.

func TestBuildZoneActivity(t *testing.T) {
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	centro := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "Centro Histórico", Active: true}
	taganga := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "Taganga", Active: true}
	vacia := models.Zone{ID: uuid.New(), City: "Santa Marta", Name: "El Rodadero", Active: true}
	boca := models.Zone{ID: uuid.New(), City: "Cartagena", Name: "Bocagrande", Active: true}
	zones := []models.Zone{taganga, centro, vacia, boca}
	ent := func(z *models.Zone, city string, age time.Duration) ActivityEntity {
		e := ActivityEntity{ID: uuid.New(), City: city, CreatedAt: now.Add(-age), Latitude: 11.24, Longitude: -74.21}
		if z != nil {
			id := z.ID
			e.ZoneID = &id
		}
		return e
	}
	old := 400 * 24 * time.Hour
	c1, c2, c3 := ent(&centro, "Santa Marta", old), ent(&centro, "santa marta", old), ent(&centro, "Sta. Marta", 5*24*time.Hour) // texto distinto: cuenta por su zona
	t1, t2 := ent(&taganga, "Santa Marta", 3*24*time.Hour), ent(&taganga, "Santa Marta", 10*24*time.Hour)
	sinZona := ent(nil, "Santa Marta", time.Hour)
	otra := ent(&boca, "Cartagena", time.Hour)
	ents := []ActivityEntity{c1, c2, c3, t1, t2, sinZona, otra}
	offers := []ActivityOffer{
		{OfferID: uuid.New(), EntityID: c1.ID, CreatedAt: now.Add(-30 * time.Minute)}, // fresca
		{OfferID: uuid.New(), EntityID: c2.ID, CreatedAt: now.Add(-5 * time.Hour)},    // vigente, no fresca
		{OfferID: uuid.New(), EntityID: sinZona.ID, CreatedAt: now.Add(-10 * time.Minute)},
		{OfferID: uuid.New(), EntityID: otra.ID, CreatedAt: now.Add(-time.Minute)}, // otra ciudad: nada
	}
	act, fresh := BuildZoneActivity(zones, "Santa Marta", ents, offers, now)
	if len(act) != 2 || act[0].Zone.ID != centro.ID || act[1].Zone.ID != taganga.ID {
		t.Fatalf("zonas: %+v (sin El Rodadero, que no tiene negocios; sin Bocagrande, de otra ciudad; en orden alfabético)", act)
	}
	if act[0].Businesses != 3 || !act[0].HasOffers || act[0].Growing {
		t.Fatalf("centro: %+v (3 negocios, con ofertas; 1 nuevo no basta para «crece»)", act[0])
	}
	if act[1].Businesses != 2 || act[1].HasOffers || !act[1].Growing {
		t.Fatalf("taganga: %+v (2 nuevos en 30 días: crece; sin ofertas)", act[1])
	}
	if len(fresh) != 2 || fresh[0].EntityID != sinZona.ID || fresh[1].EntityID != c1.ID {
		t.Fatalf("pulsos: %+v (la más nueva primero; la de 5 h y la de otra ciudad no)", fresh)
	}
	// Una oferta «del futuro» (reloj desfasado) no hace pulso.
	if _, f := BuildZoneActivity(zones, "Santa Marta", ents, []ActivityOffer{{OfferID: uuid.New(), EntityID: c1.ID, CreatedAt: now.Add(5 * time.Minute)}}, now); len(f) != 0 {
		t.Fatal("oferta con fecha futura")
	}
	// Tope de pulsos.
	many := []ActivityOffer{}
	for i := 0; i < FreshOfferMax+5; i++ {
		many = append(many, ActivityOffer{OfferID: uuid.New(), EntityID: c1.ID, CreatedAt: now.Add(-time.Duration(i) * time.Minute)})
	}
	if _, f := BuildZoneActivity(zones, "Santa Marta", ents, many, now); len(f) != FreshOfferMax {
		t.Fatalf("tope: %d", len(f))
	}
}

func TestZoneActivityIntegration(t *testing.T) {
	db := testDB(t)
	if err := db.AutoMigrate(&models.Offer{}, &models.OfferItem{}, &models.Zone{}); err != nil {
		t.Fatal(err)
	}
	owner := models.User{Name: "D", Email: "d@x.co", PasswordHash: "x"}
	db.Create(&owner)
	cat := models.Category{Name: "Comida"}
	db.Create(&cat)
	centro := models.Zone{City: "Santa Marta", Name: "Centro", Latitude: 11.24, Longitude: -74.21, RadiusM: 1200, Active: true}
	db.Create(&centro)
	mk := func(name string, zone *models.Zone) models.Entity {
		e := models.Entity{OwnerID: owner.ID, Name: name, CategoryID: cat.ID, City: "Santa Marta", Latitude: 11.24, Longitude: -74.21}
		if zone != nil {
			e.ZoneID = &zone.ID
		}
		if err := db.Omit("Owner", "Category").Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e
	}
	a, b := mk("A", &centro), mk("B", &centro)
	mk("Sin zona", nil)
	now := time.Now()
	o := models.Offer{EntityID: a.ID, Type: models.OfferHours, Title: "Nueva", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), TotalSlots: 5}
	if err := db.Omit("Entity", "Items").Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	ended := models.Offer{EntityID: b.ID, Type: models.OfferHours, Title: "Vencida", StartsAt: now.Add(-3 * time.Hour), EndsAt: now.Add(-time.Hour), TotalSlots: 5}
	db.Omit("Entity", "Items").Create(&ended)

	svc := NewZoneActivityService(db)
	city, act, fresh, err := svc.For(11.2401, -74.2101, true, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if city != "Santa Marta" || len(act) != 1 || act[0].Businesses != 2 || !act[0].HasOffers || !act[0].Growing {
		t.Fatalf("actividad: %q %+v", city, act)
	}
	if len(fresh) != 1 || fresh[0].OfferID != o.ID {
		t.Fatalf("pulsos: %+v (la vencida no)", fresh)
	}
	// Sin ubicación ni ciudad: nada (ninguna ciudad por defecto).
	if c, a2, f2, _ := svc.For(0, 0, false, "", now); c != "" || len(a2) != 0 || len(f2) != 0 {
		t.Fatalf("sin ciudad: %q", c)
	}
}
