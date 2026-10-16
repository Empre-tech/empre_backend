package services

import (
	"log"

	"empre_backend/internal/models"

	"gorm.io/gorm"
)

// SeedZones crea las zonas de Santa Marta si la tabla está vacía. Son círculos aproximados y
// curados a mano; el equipo las ajusta o añade en la base de datos (son datos, no código).
func SeedZones(db *gorm.DB) error {
	var n int64
	if err := db.Model(&models.Zone{}).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	zones := []models.Zone{
		{City: "Santa Marta", Name: "Centro Histórico", Latitude: 11.2408, Longitude: -74.2110, RadiusM: 1200, Active: true},
		{City: "Santa Marta", Name: "El Rodadero", Latitude: 11.2058, Longitude: -74.2271, RadiusM: 1500, Active: true},
		{City: "Santa Marta", Name: "Taganga", Latitude: 11.2686, Longitude: -74.1917, RadiusM: 1200, Active: true},
	}
	if err := db.Create(&zones).Error; err != nil {
		return err
	}
	log.Println("Zonas de Santa Marta sembradas")
	return nil
}

// MigrateEntityZones asigna zone_id a los negocios que aún no tienen. Idempotente.
func MigrateEntityZones(db *gorm.DB) error {
	zones, err := NewZoneService(db).List("")
	if err != nil || len(zones) == 0 {
		return err
	}
	var entities []models.Entity
	if err := db.Select("id", "latitude", "longitude").
		Where("latitude <> 0 AND longitude <> 0 AND zone_id IS NULL").Find(&entities).Error; err != nil {
		return err
	}
	for _, e := range entities {
		z := NearestZone(zones, e.Latitude, e.Longitude)
		if z == nil {
			continue
		}
		if err := db.Model(&models.Entity{}).Where("id = ?", e.ID).Update("zone_id", z.ID).Error; err != nil {
			return err
		}
	}
	return nil
}
