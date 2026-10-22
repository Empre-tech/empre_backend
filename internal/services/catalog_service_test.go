package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func intp(n int) *int { return &n }

func TestValidateServiceInput(t *testing.T) {
	good, err := ValidateServiceInput(ServiceInput{Name: "  Corte clásico ", Description: " Con tijera ", Price: intp(25000), PriceFrom: true})
	if err != nil || good.Name != "Corte clásico" || good.Description != "Con tijera" || good.Price == nil || !good.PriceFrom {
		t.Fatalf("válido: %v %+v", err, good)
	}
	noPrice, err := ValidateServiceInput(ServiceInput{Name: "Barba", PriceFrom: true})
	if err != nil || noPrice.Price != nil || noPrice.PriceFrom {
		t.Fatalf("«desde» sin precio no tiene sentido y se descarta: %v %+v", err, noPrice)
	}
	if free, err := ValidateServiceInput(ServiceInput{Name: "Revisión", Price: intp(0)}); err != nil || free.Price == nil || *free.Price != 0 {
		t.Fatalf("precio 0 (gratis) es válido y distinto de «sin precio»: %v %+v", err, free)
	}
	bad := map[string]ServiceInput{
		"nombre vacío":         {Name: "   "},
		"nombre de 1 letra":    {Name: "a"},
		"nombre largo":         {Name: strings.Repeat("x", 81)},
		"descripción larga":    {Name: "Corte", Description: strings.Repeat("x", 241)},
		"precio negativo":      {Name: "Corte", Price: intp(-1)},
		"precio sobre el tope": {Name: "Corte", Price: intp(MaxServicePrice + 1)},
		"sección larga":        {Name: "Corte", Section: strings.Repeat("x", 41)},
	}
	for name, in := range bad {
		if _, err := ValidateServiceInput(in); !errors.Is(err, ErrCatalogInvalid) {
			t.Errorf("%s: debía dar ErrCatalogInvalid, dio %v", name, err)
		}
	}
	if s, err := ValidateServiceInput(ServiceInput{Name: "Jugo", Section: "  Bebidas   naturales "}); err != nil || s.Section != "Bebidas naturales" {
		t.Fatalf("la sección se limpia de espacios: %v %q", err, s.Section)
	}
	if s, err := ValidateServiceInput(ServiceInput{Name: "Jugo"}); err != nil || s.Section != "" {
		t.Fatalf("sin sección es válido: %v %q", err, s.Section)
	}
	if _, err := ValidateServiceInput(ServiceInput{Name: "Jugo", Section: strings.Repeat("ñ", 40)}); err != nil {
		t.Errorf("40 letras con tilde caben en la sección: %v", err)
	}
	if _, err := ValidateServiceInput(ServiceInput{Name: strings.Repeat("ñ", 80)}); err != nil {
		t.Errorf("80 letras con tilde caben (se cuentan letras, no bytes): %v", err)
	}
}

func TestPriceFreshness(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	days := func(n int) *time.Time { d := now.AddDate(0, 0, -n); return &d }
	cases := []struct {
		name string
		it   models.ServiceItem
		want bool
	}{
		{"sin fechas", models.ServiceItem{}, false},
		{"precio de hace 10 días", models.ServiceItem{PriceUpdatedAt: days(10)}, true},
		{"precio de hace 90 días", models.ServiceItem{PriceUpdatedAt: days(90)}, true},
		{"precio de hace 91 días", models.ServiceItem{PriceUpdatedAt: days(91)}, false},
		{"viejo pero confirmado hace 5", models.ServiceItem{PriceUpdatedAt: days(200), ConfirmedAt: days(5)}, true},
		{"confirmado hace 200 y precio reciente", models.ServiceItem{PriceUpdatedAt: days(3), ConfirmedAt: days(200)}, true},
		{"solo confirmado hace 120", models.ServiceItem{ConfirmedAt: days(120)}, false},
	}
	for _, c := range cases {
		if got := PriceFreshness(c.it, now); got != c.want {
			t.Errorf("%s: %v, quería %v", c.name, got, c.want)
		}
	}
}

func TestCatalog_FromDatabase(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	other := newUser(t, db)
	cat := models.Category{Name: "Barberías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(ownerID uuid.UUID, name string) models.Entity {
		e := models.Entity{OwnerID: ownerID, Name: name, CategoryID: cat.ID, Latitude: 11.24, Longitude: -74.21}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
		return e
	}
	shop := mk(owner, "La Navaja")
	rival := mk(other, "Rival")
	svc := NewCatalogService(db)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

	// Crear: solo el dueño; con y sin precio.
	corte, err := svc.Create(shop.ID, owner, ServiceInput{Name: "Corte clásico", Price: intp(25000)}, t0)
	if err != nil || corte.PriceUpdatedAt == nil || corte.Status != models.ServiceActive {
		t.Fatalf("crear con precio: %v %+v", err, corte)
	}
	barba, err := svc.Create(shop.ID, owner, ServiceInput{Name: "Arreglo de barba"}, t0)
	if err != nil || barba.Price != nil || barba.PriceUpdatedAt != nil {
		t.Fatalf("sin precio: el precio queda desconocido (nil), no 0: %v %+v", err, barba)
	}
	if _, err := svc.Create(shop.ID, other, ServiceInput{Name: "Intruso"}, t0); !errors.Is(err, ErrCatalogNotOwner) {
		t.Fatalf("otro usuario no crea en mi negocio: %v", err)
	}
	if _, err := svc.Create(uuid.New(), owner, ServiceInput{Name: "Fantasma"}, t0); !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("negocio inexistente: %v", err)
	}
	if _, err := svc.Create(shop.ID, owner, ServiceInput{Name: "x"}, t0); !errors.Is(err, ErrCatalogInvalid) {
		t.Fatalf("datos inválidos: %v", err)
	}

	// Editar el nombre NO cambia la fecha del precio; editar el precio sí.
	t1 := t0.AddDate(0, 0, 20)
	upd, err := svc.Update(shop.ID, corte.ID, owner, ServiceInput{Name: "Corte clásico premium", Price: intp(25000)}, t1)
	if err != nil || upd.Name != "Corte clásico premium" || !upd.PriceUpdatedAt.Equal(t0) {
		t.Fatalf("cambiar solo el nombre no toca la fecha del precio: %v %+v", err, upd)
	}
	t2 := t0.AddDate(0, 0, 40)
	upd, err = svc.Update(shop.ID, corte.ID, owner, ServiceInput{Name: "Corte clásico premium", Price: intp(28000), PriceFrom: true}, t2)
	if err != nil || *upd.Price != 28000 || !upd.PriceFrom || !upd.PriceUpdatedAt.Equal(t2) {
		t.Fatalf("cambiar el precio actualiza su fecha: %v %+v", err, upd)
	}
	// Quitar el precio lo deja desconocido y sin fecha (y se guarda de verdad, no se ignora).
	upd, err = svc.Update(shop.ID, barba.ID, owner, ServiceInput{Name: "Arreglo de barba", Price: intp(12000)}, t1)
	if err != nil || *upd.Price != 12000 {
		t.Fatalf("poner precio: %v", err)
	}
	upd, err = svc.Update(shop.ID, barba.ID, owner, ServiceInput{Name: "Arreglo de barba"}, t2)
	if err != nil || upd.Price != nil || upd.PriceUpdatedAt != nil {
		t.Fatalf("quitar el precio: %v %+v", err, upd)
	}
	var stored models.ServiceItem
	if err := db.First(&stored, "id = ?", barba.ID).Error; err != nil || stored.Price != nil || stored.PriceUpdatedAt != nil {
		t.Fatalf("lo guardado debe tener precio nulo: %v %+v", err, stored)
	}

	// La sección se guarda, se puede cambiar sin tocar la fecha del precio y se puede quitar.
	upd, err = svc.Update(shop.ID, corte.ID, owner, ServiceInput{Name: "Corte clásico premium", Price: intp(28000), PriceFrom: true, Section: " Cortes "}, t2.AddDate(0, 0, 1))
	if err != nil || upd.Section != "Cortes" || !upd.PriceUpdatedAt.Equal(t2) {
		t.Fatalf("poner sección: %v %+v", err, upd)
	}
	var withSection models.ServiceItem
	if err := db.First(&withSection, "id = ?", corte.ID).Error; err != nil || withSection.Section != "Cortes" {
		t.Fatalf("la sección debe quedar guardada: %v %q", err, withSection.Section)
	}
	if _, err := svc.Update(shop.ID, corte.ID, owner, ServiceInput{Name: "Corte clásico premium", Price: intp(28000), PriceFrom: true}, t2); err != nil {
		t.Fatal(err)
	}
	var cleared models.ServiceItem
	if err := db.First(&cleared, "id = ?", corte.ID).Error; err != nil || cleared.Section != "" {
		t.Fatalf("quitar la sección la deja vacía (se guarda de verdad): %v %q", err, cleared.Section)
	}
	conSeccion, err := svc.Create(shop.ID, owner, ServiceInput{Name: "Afeitado", Section: "Barba"}, t0)
	if err != nil || conSeccion.Section != "Barba" {
		t.Fatalf("crear con sección: %v %+v", err, conSeccion)
	}
	if err := svc.Retire(shop.ID, conSeccion.ID, owner); err != nil {
		t.Fatal(err)
	}

	// Un negocio ajeno no edita ni retira mis servicios, ni uno sirve con el id de otro negocio.
	if _, err := svc.Update(shop.ID, corte.ID, other, ServiceInput{Name: "Robado"}, t1); !errors.Is(err, ErrCatalogNotOwner) {
		t.Fatalf("editar ajeno: %v", err)
	}
	if err := svc.Retire(shop.ID, corte.ID, other); !errors.Is(err, ErrCatalogNotOwner) {
		t.Fatalf("retirar ajeno: %v", err)
	}
	if _, err := svc.Update(rival.ID, corte.ID, other, ServiceInput{Name: "Cruzado"}, t1); !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("un servicio no se edita a través de otro negocio: %v", err)
	}

	// Lectura pública: solo activos, en orden.
	list, err := svc.List(shop.ID)
	if err != nil || len(list) != 2 || list[0].Name != "Corte clásico premium" || list[1].Name != "Arreglo de barba" {
		t.Fatalf("lista: %v %+v", err, list)
	}
	if other, _ := svc.List(rival.ID); len(other) != 0 {
		t.Fatalf("el catálogo de otro negocio no se mezcla: %+v", other)
	}

	// Confirmar: marca el catálogo activo como vigente.
	if n, err := svc.Confirm(shop.ID, owner, t2); err != nil || n != 2 {
		t.Fatalf("confirmar: %v n=%d", err, n)
	}
	if _, err := svc.Confirm(shop.ID, other, t2); !errors.Is(err, ErrCatalogNotOwner) {
		t.Fatalf("confirmar ajeno: %v", err)
	}
	list, _ = svc.List(shop.ID)
	if list[0].ConfirmedAt == nil || !PriceFreshness(list[1], t2.AddDate(0, 0, 10)) {
		t.Fatalf("tras confirmar queda vigente: %+v", list)
	}

	// Retirar: deja de mostrarse y no se puede editar ni retirar de nuevo; el registro se conserva.
	if err := svc.Retire(shop.ID, corte.ID, owner); err != nil {
		t.Fatalf("retirar: %v", err)
	}
	if err := svc.Retire(shop.ID, corte.ID, owner); !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("retirar dos veces: %v", err)
	}
	if _, err := svc.Update(shop.ID, corte.ID, owner, ServiceInput{Name: "Revivido"}, t2); !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("un servicio retirado no se edita: %v", err)
	}
	list, _ = svc.List(shop.ID)
	if len(list) != 1 || list[0].ID != barba.ID {
		t.Fatalf("retirado no se muestra: %+v", list)
	}
	var kept int64
	db.Model(&models.ServiceItem{}).Where("id = ? AND status = ?", corte.ID, models.ServiceRetired).Count(&kept)
	if kept != 1 {
		t.Fatalf("el historial se conserva: %d", kept)
	}
}

func TestCatalog_LimitAndDatabaseConstraint(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cat := models.Category{Name: "Tiendas"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	shop := models.Entity{OwnerID: owner, Name: "Tienda", CategoryID: cat.ID}
	if err := db.Omit(clause.Associations).Create(&shop).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewCatalogService(db)
	now := time.Now()
	var first *models.ServiceItem
	for i := 0; i < MaxActiveServices; i++ {
		it, err := svc.Create(shop.ID, owner, ServiceInput{Name: "Servicio " + strings.Repeat("a", i%5+2)}, now)
		if err != nil {
			t.Fatalf("servicio %d: %v", i, err)
		}
		if first == nil {
			first = it
		}
	}
	if _, err := svc.Create(shop.ID, owner, ServiceInput{Name: "Uno de más"}, now); !errors.Is(err, ErrCatalogLimit) {
		t.Fatalf("el catálogo tiene tope: %v", err)
	}
	// Retirar uno libera un lugar: el tope cuenta solo los activos.
	if err := svc.Retire(shop.ID, first.ID, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(shop.ID, owner, ServiceInput{Name: "Cabe de nuevo"}, now); err != nil {
		t.Fatalf("tras retirar uno debe caber otro: %v", err)
	}

	// La base de datos rechaza un precio inválido aunque se salte el servicio (no depende solo de la pantalla).
	neg := -5
	bad := models.ServiceItem{EntityID: shop.ID, Name: "Directo", Price: &neg, Status: models.ServiceActive}
	if err := db.Create(&bad).Error; err == nil {
		t.Fatal("el CHECK de la base debe rechazar un precio negativo")
	}
	huge := MaxServicePrice + 1
	bad = models.ServiceItem{EntityID: shop.ID, Name: "Directo", Price: &huge, Status: models.ServiceActive}
	if err := db.Create(&bad).Error; err == nil {
		t.Fatal("el CHECK de la base debe rechazar un precio sobre el tope")
	}
}
