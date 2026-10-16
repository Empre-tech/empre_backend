package services

import (
	"strings"

	"empre_backend/internal/models"
	"empre_backend/pkg/utils"

	"gorm.io/gorm"
)

// ZoneService lee las zonas por ciudad y le asigna a cada negocio la suya al guardarlo.
type ZoneService struct {
	DB *gorm.DB
}

func NewZoneService(db *gorm.DB) *ZoneService {
	return &ZoneService{DB: db}
}

// List devuelve las zonas activas de una ciudad (sin distinguir mayúsculas ni espacios
// sobrantes). Con la ciudad vacía devuelve las de todas.
func (s *ZoneService) List(city string) ([]models.Zone, error) {
	var zones []models.Zone
	q := s.DB.Where("active = ?", true).Order("city, name")
	if c := strings.TrimSpace(city); c != "" {
		q = q.Where("LOWER(city) = LOWER(?)", c)
	}
	if err := q.Find(&zones).Error; err != nil {
		return nil, err
	}
	return zones, nil
}

// NearestZone devuelve la zona que contiene el punto; si lo contienen varias, la de centro
// más cercano. Nil si ninguna (o si el punto es 0,0, que es «sin ubicación»).
func NearestZone(zones []models.Zone, lat, lng float64) *models.Zone {
	if lat == 0 && lng == 0 {
		return nil
	}
	var best *models.Zone
	bestD := 0.0
	for i := range zones {
		z := &zones[i]
		if !z.Active {
			continue
		}
		d := utils.DistanceMeters(lat, lng, z.Latitude, z.Longitude)
		if d > float64(z.RadiusM) {
			continue
		}
		if best == nil || d < bestD {
			best, bestD = z, d
		}
	}
	return best
}

// Assign pone en el negocio la zona donde queda (o la quita si ya no queda en ninguna).
// Se llama al crear y al editar, así la consulta del mapa nunca calcula zonas. La ubicación
// manda sobre el texto de la ciudad, que puede estar escrito de otra forma, así que se
// prueban las zonas de todas las ciudades.
func (s *ZoneService) Assign(e *models.Entity) error {
	zones, err := s.List("")
	if err != nil {
		return err
	}
	if z := NearestZone(zones, e.Latitude, e.Longitude); z != nil {
		id := z.ID
		e.ZoneID = &id
	} else {
		e.ZoneID = nil
	}
	return nil
}
