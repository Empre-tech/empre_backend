package services

import (
	"errors"
	"testing"
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

func TestCellSize(t *testing.T) {
	cases := []struct{ delta, want float64 }{
		{0.012, 0.0025},
		{0.03, 0.005},
		{0.1, 0.02},
		{0.5, 0.16},
		{3, 0.64},
		{100, 1.28}, // más grande que todo: la mayor casilla
		{0, 1.28},   // sin dato: la mayor, nunca cero
		{-1, 1.28},
	}
	for _, c := range cases {
		if got := CellSize(c.delta); got != c.want {
			t.Errorf("CellSize(%v) = %v, quería %v", c.delta, got, c.want)
		}
	}
	// Estable: un cambio pequeño de zoom no cambia la casilla.
	if CellSize(0.10) != CellSize(0.11) {
		t.Error("las casillas no deben cambiar con un zoom casi igual")
	}
	// Nunca más de CellsPerScreen casillas de alto (salvo en el tope superior).
	for d := 0.01; d < 5; d *= 1.3 {
		if d/CellSize(d) > CellsPerScreen+1e-9 {
			t.Errorf("a %v caben más de %d casillas", d, CellsPerScreen)
		}
	}
}

func TestCells_FromDatabase(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	now := time.Now()
	cafe := models.Category{Name: "Cafeterías"}
	barber := models.Category{Name: "Barberías"}
	for _, c := range []*models.Category{&cafe, &barber} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk := func(name string, cat uuid.UUID, lat, lng float64) models.Entity {
		e := models.Entity{OwnerID: owner, Name: name, CategoryID: cat, Latitude: lat, Longitude: lng}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatalf("crear %s: %v", name, err)
		}
		return e
	}
	offer := func(e models.Entity, ends time.Time, total, used int) {
		o := models.Offer{EntityID: e.ID, Type: models.OfferHours, Title: "Oferta", Price: "1", StartsAt: now.Add(-time.Hour), EndsAt: ends, TotalSlots: total, UsedSlots: used, RadiusMeters: 500, RepeatPolicy: models.RepeatOnce}
		if err := db.Omit(clause.Associations).Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}

	// Casilla A (lat 11.20–11.205): 5 cafeterías y 1 barbería, con una oferta vigente.
	var a []models.Entity
	for i := 0; i < 5; i++ {
		a = append(a, mk("Café A", cafe.ID, 11.2010+float64(i)*0.0002, -74.2010))
	}
	a = append(a, mk("Barbería A", barber.ID, 11.2030, -74.2020))
	offer(a[1], now.Add(time.Hour), 5, 0)
	// Casilla B (a ~6 km): 3 barberías; sus ofertas no cuentan (vencida y sin cupos).
	var b []models.Entity
	for i := 0; i < 3; i++ {
		b = append(b, mk("Barbería B", barber.ID, 11.2610+float64(i)*0.0002, -74.2010))
	}
	offer(b[0], now.Add(-time.Minute), 5, 0)
	offer(b[1], now.Add(time.Hour), 3, 3)
	// Fuera del rectángulo, sin ubicación y borrado: no cuentan.
	mk("Lejos", cafe.ID, 12.5, -74.2)
	mk("Sin ubicación", cafe.ID, 0, 0)
	gone := mk("Borrado", cafe.ID, 11.2015, -74.2012)
	if err := db.Delete(&gone).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewCellService(db)
	q := CellQuery{MinLat: 11.15, MaxLat: 11.30, MinLng: -74.30, MaxLng: -74.10, LatDelta: 0.15, Now: now}
	cells, size, truncated, err := svc.Cells(q)
	if err != nil || truncated {
		t.Fatalf("consulta: %v truncada=%v", err, truncated)
	}
	if size != CellSize(0.15) {
		t.Fatalf("tamaño de casilla: %v", size)
	}
	total := 0
	for _, c := range cells {
		total += c.Count
	}
	if total != 9 {
		t.Fatalf("deben contarse 9 negocios (no fuera, sin ubicación ni borrados): %d en %+v", total, cells)
	}
	// Orden: la más poblada primero; categoría dominante; oferta solo con oferta vigente y con cupos.
	first := cells[0]
	if first.Count != 6 || first.CategoryID == nil || *first.CategoryID != cafe.ID || !first.HasOffer {
		t.Fatalf("casilla A: %+v", first)
	}
	if len(cells) != 2 || cells[1].Count != 3 || cells[1].HasOffer || cells[1].CategoryID == nil || *cells[1].CategoryID != barber.ID {
		t.Fatalf("casilla B: %+v", cells)
	}
	// El centro de la casilla es el promedio de sus negocios.
	if first.Latitude < 11.201 || first.Latitude > 11.204 {
		t.Fatalf("latitud promedio: %v", first.Latitude)
	}

	// Filtro de categoría.
	q.CategoryID = barber.ID.String()
	cells, _, _, _ = svc.Cells(q)
	total = 0
	for _, c := range cells {
		total += c.Count
	}
	if total != 4 {
		t.Fatalf("solo barberías: %d en %+v", total, cells)
	}
	q.CategoryID = ""

	// A zoom más cercano las casillas son más finas: se separan.
	q.LatDelta = 0.012
	fine, _, _, _ := svc.Cells(q)
	if len(fine) <= 2 {
		t.Fatalf("a zoom cercano debe haber más casillas: %d", len(fine))
	}

	// Un rectángulo que contiene el 0,0 (sin ubicación) no cuenta esos negocios.
	around, _, _, err := svc.Cells(CellQuery{MinLat: -1, MaxLat: 1, MinLng: -1, MaxLng: 1, LatDelta: 0.5})
	if err != nil || len(around) != 0 {
		t.Fatalf("sin ubicación no debe contarse: %v %+v", err, around)
	}

	// Sin rectángulo o con uno al revés: error claro, no toda la tabla.
	for _, bad := range []CellQuery{{}, {MinLat: 12, MaxLat: 11, MinLng: -74.3, MaxLng: -74.1}} {
		if _, _, _, err := svc.Cells(bad); !errors.Is(err, ErrCellsBox) {
			t.Fatalf("rectángulo inválido debe dar ErrCellsBox: %v", err)
		}
	}
}

func TestCells_CapAndTruncation(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cat := models.Category{Name: "Tiendas"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	// 205 negocios, cada uno en su propia casilla (separados 0.01°): se corta en MaxCells.
	for i := 0; i < MaxCells+5; i++ {
		e := models.Entity{OwnerID: owner, Name: "T", CategoryID: cat.ID, Latitude: 1 + float64(i)*0.01, Longitude: -74.2}
		if err := db.Omit(clause.Associations).Create(&e).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Casillas de 0.0025°: caben todas separadas.
	cells, _, truncated, err := NewCellService(db).Cells(CellQuery{MinLat: 0.5, MaxLat: 4, MinLng: -74.5, MaxLng: -74, LatDelta: 0.012})
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(cells) != MaxCells {
		t.Fatalf("debe cortar en %d y avisar: %d truncada=%v", MaxCells, len(cells), truncated)
	}
}
