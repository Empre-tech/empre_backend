package services

import (
	"testing"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

func zone(name string, lat, lng float64, r int) models.Zone {
	return models.Zone{ID: uuid.New(), City: "Santa Marta", Name: name, Latitude: lat, Longitude: lng, RadiusM: r, Active: true}
}

func TestNearestZone(t *testing.T) {
	centro := zone("Centro", 11.2408, -74.2110, 1200)
	rodadero := zone("Rodadero", 11.2058, -74.2271, 1500)
	zones := []models.Zone{centro, rodadero}

	if z := NearestZone(zones, 11.2410, -74.2112); z == nil || z.Name != "Centro" {
		t.Fatalf("dentro del Centro: %+v", z)
	}
	if z := NearestZone(zones, 11.2100, -74.2260); z == nil || z.Name != "Rodadero" {
		t.Fatalf("dentro del Rodadero: %+v", z)
	}
	if z := NearestZone(zones, 11.5, -74.5); z != nil {
		t.Fatalf("fuera de toda zona debe ser nil: %+v", z)
	}
	if z := NearestZone(zones, 0, 0); z != nil {
		t.Fatalf("0,0 es sin ubicación: %+v", z)
	}
	if z := NearestZone(nil, 11.24, -74.21); z != nil {
		t.Fatalf("sin zonas debe ser nil: %+v", z)
	}

	// Si lo contienen dos, gana la de centro más cercano.
	a := zone("A", 11.2400, -74.2100, 3000)
	b := zone("B", 11.2420, -74.2120, 3000)
	if z := NearestZone([]models.Zone{a, b}, 11.2419, -74.2119); z == nil || z.Name != "B" {
		t.Fatalf("debe ganar la más cercana: %+v", z)
	}

	// Una zona inactiva no cuenta.
	off := zone("Off", 11.2408, -74.2110, 1200)
	off.Active = false
	if z := NearestZone([]models.Zone{off}, 11.2408, -74.2110); z != nil {
		t.Fatalf("zona inactiva: %+v", z)
	}
}

func TestZones_FromDatabase(t *testing.T) {
	db := openTestDB(t, false)
	owner := newUser(t, db)
	cat := models.Category{Name: "Cafeterías"}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}

	centro := zone("Centro Histórico", 11.2408, -74.2110, 1200)
	inactiva := zone("Vieja", 4.7110, -74.0721, 5000)
	inactiva.City = "Bogotá"
	inactiva.Active = false
	otra := zone("Chapinero", 4.6486, -74.0628, 1500)
	otra.City = "Bogotá"
	for _, z := range []*models.Zone{&centro, &inactiva, &otra} {
		if err := db.Select("*").Create(z).Error; err != nil {
			t.Fatal(err)
		}
	}

	// GORM ignora el false de un bool con default:true al crear: se apaga con un UPDATE.
	if err := db.Model(&models.Zone{}).Where("id = ?", inactiva.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	zs := NewZoneService(db)

	// List: por ciudad sin importar mayúsculas ni espacios, sin inactivas; ciudad sin zonas = vacío.
	got, err := zs.List("  santa marta ")
	if err != nil || len(got) != 1 || got[0].Name != "Centro Histórico" {
		t.Fatalf("Santa Marta: %v %+v", err, got)
	}
	if got, _ := zs.List("Bogotá"); len(got) != 1 || got[0].Name != "Chapinero" {
		t.Fatalf("Bogotá debe traer solo la activa: %+v", got)
	}
	if got, err := zs.List("Pereira"); err != nil || len(got) != 0 {
		t.Fatalf("ciudad sin zonas: %v %+v", err, got)
	}
	if got, _ := zs.List(""); len(got) != 2 {
		t.Fatalf("sin ciudad: todas las activas: %+v", got)
	}

	// Asignar al guardar y al editar (la ubicación manda sobre el texto de la ciudad).
	svc := NewEntityService(repository.NewEntityRepository(db), nil)
	svc.Zones = zs
	e := models.Entity{OwnerID: owner, Name: "Café del Parque", CategoryID: cat.ID, City: "Santa Marta D.T.C.H.", Latitude: 11.2410, Longitude: -74.2112}
	if err := svc.CreateEntity(&e); err != nil {
		t.Fatalf("crear: %v", err)
	}
	var saved models.Entity
	if err := db.First(&saved, "id = ?", e.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ZoneID == nil || *saved.ZoneID != centro.ID {
		t.Fatalf("zone_id al crear: %v", saved.ZoneID)
	}

	// Se muda fuera de toda zona: la zona se quita.
	saved.Latitude, saved.Longitude = 11.5, -74.5
	if err := svc.UpdateEntity(&saved); err != nil {
		t.Fatalf("editar: %v", err)
	}
	var moved models.Entity
	if err := db.First(&moved, "id = ?", e.ID).Error; err != nil {
		t.Fatal(err)
	}
	if moved.ZoneID != nil {
		t.Fatalf("fuera de zonas debe quedar sin zona: %v", moved.ZoneID)
	}

	// Sin ubicación (0,0) no hay zona.
	nowhere := models.Entity{OwnerID: owner, Name: "A domicilio", CategoryID: cat.ID}
	if err := svc.CreateEntity(&nowhere); err != nil {
		t.Fatal(err)
	}
	if nowhere.ZoneID != nil {
		t.Fatalf("sin ubicación: %v", nowhere.ZoneID)
	}

	// Sin ZoneService no se asigna nada y guardar sigue funcionando.
	plain := NewEntityService(repository.NewEntityRepository(db), nil)
	p := models.Entity{OwnerID: owner, Name: "Sin servicio", CategoryID: cat.ID, Latitude: 11.2410, Longitude: -74.2112}
	if err := plain.CreateEntity(&p); err != nil || p.ZoneID != nil {
		t.Fatalf("sin servicio: %v %v", err, p.ZoneID)
	}

	// Reasignación de los que no tienen zona: idempotente.
	if err := MigrateEntityZones(db); err != nil {
		t.Fatal(err)
	}
	var back models.Entity
	db.First(&back, "id = ?", p.ID)
	if back.ZoneID == nil || *back.ZoneID != centro.ID {
		t.Fatalf("migración: %v", back.ZoneID)
	}
	if err := MigrateEntityZones(db); err != nil {
		t.Fatal(err)
	}
	var stillNone models.Entity
	db.First(&stillNone, "id = ?", moved.ID)
	if stillNone.ZoneID != nil {
		t.Fatalf("la migración no debe inventar zona a quien queda fuera: %v", stillNone.ZoneID)
	}

	// La siembra solo corre con la tabla vacía.
	if err := SeedZones(db); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&models.Zone{}).Count(&n)
	if n != 3 {
		t.Fatalf("la siembra no debe tocar una tabla con zonas: %d", n)
	}
}

func TestSeedZones_EmptyTable(t *testing.T) {
	db := openTestDB(t, false)
	if err := SeedZones(db); err != nil {
		t.Fatal(err)
	}
	zs, _ := NewZoneService(db).List("Santa Marta")
	if len(zs) != 3 {
		t.Fatalf("siembra: %+v", zs)
	}
}
