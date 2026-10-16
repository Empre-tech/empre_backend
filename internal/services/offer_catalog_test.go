package services

import (
	"strings"
	"testing"
	"time"

	"empre_backend/internal/dtos"
	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// offerFixture arma un negocio con un catálogo de tres servicios (dos con precio, uno sin) y el servicio de ofertas.
type offerFixture struct {
	owner, other uuid.UUID
	shop, rival  models.Entity
	corte, barba models.ServiceItem // con precio: 25.000 y 12.000
	cejas        models.ServiceItem // sin precio
	ajeno        models.ServiceItem // de otro negocio
	offers       *OfferService
	catalog      *CatalogService
}

func newOfferFixture(t *testing.T) *offerFixture {
	t.Helper()
	db := openTestDB(t, false)
	f := &offerFixture{owner: newUser(t, db), other: newUser(t, db)}
	cat := models.Category{Name: "Barberías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(owner uuid.UUID, name string) models.Entity {
		e := models.Entity{OwnerID: owner, Name: name, CategoryID: cat.ID, Latitude: 11.24, Longitude: -74.21}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e
	}
	f.shop, f.rival = mk(f.owner, "La Navaja"), mk(f.other, "Rival")
	f.catalog = NewCatalogService(db)
	now := time.Now()
	add := func(e models.Entity, owner uuid.UUID, name string, price *int) models.ServiceItem {
		it, err := f.catalog.Create(e.ID, owner, ServiceInput{Name: name, Price: price}, now)
		if err != nil {
			t.Fatalf("servicio %s: %v", name, err)
		}
		return *it
	}
	f.corte = add(f.shop, f.owner, "Corte clásico", intp(25000))
	f.barba = add(f.shop, f.owner, "Arreglo de barba", intp(12000))
	f.cejas = add(f.shop, f.owner, "Perfilado de cejas", nil)
	f.ajeno = add(f.rival, f.other, "Servicio ajeno", intp(5000))
	f.offers = NewOfferService(repository.NewOfferRepository(db), repository.NewEntityRepository(db))
	return f
}

func (f *offerFixture) publish(t *testing.T, in PublishOfferInput) (*models.Offer, error) {
	t.Helper()
	if in.Type == "" {
		in.Type = models.OfferDay
	}
	if in.Title == "" {
		in.Title = "Combo"
	}
	if in.TotalSlots == 0 {
		in.TotalSlots = 5
	}
	if in.EndsAt.IsZero() {
		in.EndsAt = utils.NowInCartagena().Add(24 * time.Hour)
	}
	return f.offers.Publish(f.shop.ID, f.owner, in)
}

func TestOffer_FreezesCatalogAndKeepsPricesApart(t *testing.T) {
	f := newOfferFixture(t)
	db := f.offers.OfferRepo.DB

	offer, err := f.publish(t, PublishOfferInput{ServiceItemIDs: []uuid.UUID{f.corte.ID, f.barba.ID}, SpecialPrice: intp(30000)})
	if err != nil {
		t.Fatalf("publicar: %v", err)
	}
	if offer.ReferencePrice == nil || *offer.ReferencePrice != 37000 {
		t.Fatalf("referencia = suma de los precios habituales (25.000 + 12.000): %v", offer.ReferencePrice)
	}
	if saving := dtos.OfferSaving(offer.SpecialPrice, offer.ReferencePrice); saving == nil || *saving != 7000 {
		t.Fatalf("ahorro = referencia − especial: %v", saving)
	}
	got, err := f.offers.ActiveListForEntity(f.shop.ID)
	if err != nil || len(got) != 1 || len(got[0].Items) != 2 || got[0].Items[0].Name != "Corte clásico" || *got[0].Items[1].Price != 12000 {
		t.Fatalf("la oferta activa trae sus servicios congelados en orden: %v %+v", err, got)
	}
	resp := dtos.OfferResponseFrom(&got[0])
	if resp.Saving == nil || *resp.Saving != 7000 || len(resp.Items) != 2 {
		t.Fatalf("DTO: %+v", resp)
	}

	// La oferta NO escribe en el catálogo: el precio habitual sigue intacto.
	var corte models.ServiceItem
	db.First(&corte, "id = ?", f.corte.ID)
	if corte.Price == nil || *corte.Price != 25000 {
		t.Fatalf("el precio habitual no debe cambiar por publicar una oferta: %v", corte.Price)
	}

	// Cambiar el precio del catálogo después NO altera la oferta ya publicada.
	if _, err := f.catalog.Update(f.shop.ID, f.corte.ID, f.owner, ServiceInput{Name: "Corte clásico", Price: intp(40000)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ = f.offers.ActiveListForEntity(f.shop.ID)
	if *got[0].ReferencePrice != 37000 || *got[0].Items[0].Price != 25000 {
		t.Fatalf("la copia congelada no cambia con el catálogo: ref=%v item=%v", *got[0].ReferencePrice, *got[0].Items[0].Price)
	}
	// Retirar el servicio tampoco la cambia.
	if err := f.catalog.Retire(f.shop.ID, f.barba.ID, f.owner); err != nil {
		t.Fatal(err)
	}
	got, _ = f.offers.ActiveListForEntity(f.shop.ID)
	if len(got[0].Items) != 2 || got[0].Items[1].Name != "Arreglo de barba" {
		t.Fatalf("un servicio retirado sigue en la oferta que ya lo incluía: %+v", got[0].Items)
	}
}

func TestOffer_PriceRules(t *testing.T) {
	f := newOfferFixture(t)

	// Un precio especial sin servicios es válido, pero sin referencia no hay ahorro.
	o, err := f.publish(t, PublishOfferInput{Type: models.OfferHours, SpecialPrice: intp(15000)})
	if err != nil || o.ReferencePrice != nil || dtos.OfferSaving(o.SpecialPrice, o.ReferencePrice) != nil {
		t.Fatalf("especial sin servicios: %v %+v", err, o)
	}
	// Con un servicio sin precio la referencia no existe (no se inventa): no hay ahorro.
	o, err = f.publish(t, PublishOfferInput{Type: models.OfferWeek, ServiceItemIDs: []uuid.UUID{f.corte.ID, f.cejas.ID}, SpecialPrice: intp(20000)})
	if err != nil || o.ReferencePrice != nil || dtos.OfferSaving(o.SpecialPrice, o.ReferencePrice) != nil || len(o.Items) != 2 || o.Items[1].Price != nil {
		t.Fatalf("un servicio sin precio impide la referencia: %v %+v", err, o)
	}

	bad := map[string]PublishOfferInput{
		"especial igual a la referencia": {ServiceItemIDs: []uuid.UUID{f.corte.ID}, SpecialPrice: intp(25000)},
		"especial mayor a la referencia": {ServiceItemIDs: []uuid.UUID{f.corte.ID}, SpecialPrice: intp(30000)},
		"especial negativo":              {SpecialPrice: intp(-1)},
		"especial sobre el tope":         {SpecialPrice: intp(MaxServicePrice + 1)},
		"servicio de otro negocio":       {ServiceItemIDs: []uuid.UUID{f.ajeno.ID}},
		"servicio inexistente":           {ServiceItemIDs: []uuid.UUID{uuid.New()}},
	}
	for name, in := range bad {
		// Tipos distintos no chocan con el límite de ofertas activas por tipo (se prueba la regla de precio, no el tope).
		in.Type = models.OfferHours
		before := countOffers(t, f)
		if _, err := f.publish(t, in); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
		if countOffers(t, f) != before {
			t.Errorf("%s: no debe quedar una oferta a medias", name)
		}
	}

	// Tope de servicios por oferta.
	many := []uuid.UUID{}
	for i := 0; i < MaxOfferItems+1; i++ {
		it, err := f.catalog.Create(f.shop.ID, f.owner, ServiceInput{Name: "Extra " + strings.Repeat("x", i+1), Price: intp(1000)}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		many = append(many, it.ID)
	}
	if _, err := f.publish(t, PublishOfferInput{Type: models.OfferHours, ServiceItemIDs: many}); err == nil {
		t.Error("más de 5 servicios debe rechazarse")
	}
	// Un servicio repetido cuenta una vez.
	if o, err := f.publish(t, PublishOfferInput{Type: models.OfferHours, ServiceItemIDs: []uuid.UUID{f.corte.ID, f.corte.ID}}); err != nil || len(o.Items) != 1 {
		t.Errorf("duplicados: %v %+v", err, o)
	}
}

func countOffers(t *testing.T, f *offerFixture) int64 {
	t.Helper()
	var n int64
	f.offers.OfferRepo.DB.Model(&models.Offer{}).Where("entity_id = ?", f.shop.ID).Count(&n)
	return n
}

func TestOffer_DatabaseRejectsBadPrices(t *testing.T) {
	f := newOfferFixture(t)
	db := f.offers.OfferRepo.DB
	mk := func(special, ref *int) error {
		o := models.Offer{EntityID: f.shop.ID, Type: models.OfferWeek, Title: "Directo", StartsAt: time.Now(), EndsAt: time.Now().Add(time.Hour), TotalSlots: 1, SpecialPrice: special, ReferencePrice: ref, RepeatPolicy: models.RepeatOnce}
		return db.Omit(clause.Associations).Create(&o).Error
	}
	if err := mk(intp(10), intp(20)); err != nil {
		t.Fatalf("un precio válido debe entrar: %v", err)
	}
	for name, c := range map[string][2]*int{
		"especial igual a la referencia": {intp(20), intp(20)},
		"especial mayor":                 {intp(30), intp(20)},
		"especial negativo":              {intp(-1), nil},
		"especial sobre el tope":         {intp(MaxServicePrice + 1), nil},
		"referencia cero":                {nil, intp(0)},
	} {
		if err := mk(c[0], c[1]); err == nil {
			t.Errorf("%s: el CHECK de la base debe rechazarlo", name)
		}
	}
}

func TestOffer_RepublishRefreezesCurrentPrices(t *testing.T) {
	f := newOfferFixture(t)
	first, err := f.publish(t, PublishOfferInput{ServiceItemIDs: []uuid.UUID{f.corte.ID, f.barba.ID}, SpecialPrice: intp(30000)})
	if err != nil {
		t.Fatal(err)
	}
	// Termina la oferta, sube el precio de un servicio y se retira el otro.
	f.offers.OfferRepo.DB.Model(&models.Offer{}).Where("id = ?", first.ID).Update("ends_at", time.Now().Add(-time.Hour))
	if _, err := f.catalog.Update(f.shop.ID, f.corte.ID, f.owner, ServiceInput{Name: "Corte clásico", Price: intp(35000)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.catalog.Retire(f.shop.ID, f.barba.ID, f.owner); err != nil {
		t.Fatal(err)
	}

	again, err := f.offers.Republish(first.ID, f.owner, PublishOfferInput{
		TotalSlots: 3, EndsAt: utils.NowInCartagena().Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("republicar: %v", err)
	}
	if len(again.Items) != 1 || again.Items[0].Name != "Corte clásico" || *again.Items[0].Price != 35000 {
		t.Fatalf("se vuelve a congelar con el precio de hoy y sin el servicio retirado: %+v", again.Items)
	}
	if again.ReferencePrice == nil || *again.ReferencePrice != 35000 || again.SpecialPrice == nil || *again.SpecialPrice != 30000 {
		t.Fatalf("referencia nueva y mismo precio especial: ref=%v special=%v", again.ReferencePrice, again.SpecialPrice)
	}
	// La oferta original conserva su copia.
	var old models.OfferItem
	if err := f.offers.OfferRepo.DB.Where("offer_id = ? AND name = ?", first.ID, "Arreglo de barba").First(&old).Error; err != nil {
		t.Fatalf("la oferta anterior conserva sus servicios: %v", err)
	}
}

func TestOffer_QuickActionKeepsItems(t *testing.T) {
	f := newOfferFixture(t)
	o, err := f.publish(t, PublishOfferInput{ServiceItemIDs: []uuid.UUID{f.corte.ID}, SpecialPrice: intp(20000)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.offers.QuickAction(o.ID, f.owner, "slots", 2); err != nil {
		t.Fatal(err)
	}
	got, _ := f.offers.ActiveListForEntity(f.shop.ID)
	if len(got) != 1 || got[0].TotalSlots != 7 || len(got[0].Items) != 1 || got[0].SpecialPrice == nil || *got[0].SpecialPrice != 20000 {
		t.Fatalf("una acción rápida no toca precio ni servicios: %+v", got)
	}
}
