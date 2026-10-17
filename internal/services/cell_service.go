package services

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
)

// Casillas del mapa (F10): a distancia la app no necesita cada negocio sino cuántos hay por
// zona. El servidor agrupa por casillas de latitud y longitud y devuelve solo el conteo, si
// alguno tiene oferta vigente y la categoría que más se repite; así a zoom lejano el mapa
// dibuja decenas de burbujas en vez de miles de pines.

const (
	// CellsPerScreen es cuántas casillas caben aproximadamente a lo alto de la pantalla.
	CellsPerScreen = 6
	// MaxCells es el tope de casillas por respuesta (las más pobladas primero).
	MaxCells = 200
)

// cellSteps son los tamaños de casilla permitidos (en grados). Se redondea hacia uno de ellos
// para que las casillas no cambien de forma con cada pequeño cambio de zoom (las burbujas
// no «saltan» al acercar o alejar un poco).
var cellSteps = []float64{0.0025, 0.005, 0.01, 0.02, 0.04, 0.08, 0.16, 0.32, 0.64, 1.28}

// CellSize devuelve el tamaño de casilla (grados) para un mapa que muestra latDelta grados de
// alto: la menor de las medidas permitidas que dé como mucho CellsPerScreen casillas.
func CellSize(latDelta float64) float64 {
	if latDelta <= 0 || math.IsNaN(latDelta) {
		return cellSteps[len(cellSteps)-1]
	}
	want := latDelta / CellsPerScreen
	for _, s := range cellSteps {
		if s >= want {
			return s
		}
	}
	return cellSteps[len(cellSteps)-1]
}

// Cell es una casilla con negocios.
type Cell struct {
	Latitude   float64    `json:"latitude"`
	Longitude  float64    `json:"longitude"`
	Count      int        `json:"count"`
	HasOffer   bool       `json:"has_offer"`
	CategoryID *uuid.UUID `json:"category_id,omitempty"`
}

// CellQuery son los filtros de la consulta: el rectángulo visible es obligatorio.
type CellQuery struct {
	MinLat, MaxLat, MinLng, MaxLng float64
	LatDelta                       float64
	CategoryID                     string
	SubcategoryID                  string
	Now                            time.Time
}

type CellService struct {
	DB *gorm.DB
}

func NewCellService(db *gorm.DB) *CellService {
	return &CellService{DB: db}
}

var ErrCellsBox = errors.New("se necesita el rectángulo visible (min_lat, max_lat, min_lng, max_lng)")

// Cells agrupa por casillas los negocios del rectángulo. Devuelve las casillas, el tamaño usado y
// si se cortó por el tope.
func (s *CellService) Cells(q CellQuery) ([]Cell, float64, bool, error) {
	if q.MinLat == 0 && q.MaxLat == 0 && q.MinLng == 0 && q.MaxLng == 0 {
		return nil, 0, false, ErrCellsBox
	}
	if q.MinLat >= q.MaxLat || q.MinLng >= q.MaxLng {
		return nil, 0, false, ErrCellsBox
	}
	if q.Now.IsZero() {
		q.Now = time.Now()
	}
	latDelta := q.LatDelta
	if latDelta <= 0 {
		latDelta = q.MaxLat - q.MinLat
	}
	size := CellSize(latDelta)

	db := s.DB.Model(&models.Entity{}).
		Select(`AVG(entities.latitude) AS latitude, AVG(entities.longitude) AS longitude, COUNT(*) AS count,
			BOOL_OR(EXISTS (SELECT 1 FROM offers o WHERE o.entity_id = entities.id AND o.deleted_at IS NULL
				AND o.ends_at > ? AND o.used_slots < o.total_slots)) AS has_offer,
			MODE() WITHIN GROUP (ORDER BY entities.category_id) AS category_id`, q.Now).
		Where("entities.latitude <> 0 OR entities.longitude <> 0").
		Where("entities.latitude BETWEEN ? AND ?", q.MinLat, q.MaxLat).
		Where("entities.longitude BETWEEN ? AND ?", q.MinLng, q.MaxLng)
	db = repository.WhereCategories(db, "entities.category_id", q.CategoryID)
	if q.SubcategoryID != "" {
		db = db.Where(
			"EXISTS (SELECT 1 FROM entity_subcategories es WHERE es.entity_id = entities.id AND es.subcategory_id = ?)",
			q.SubcategoryID,
		)
	}
	// Mismo criterio que el listado: no se cuentan los negocios que superaron el umbral gratis.
	db = db.Where(
		`NOT EXISTS (
			SELECT 1 FROM subscriptions sub
			WHERE sub.entity_id = entities.id
				AND sub.status != 'active'
				AND sub.trial_threshold_notified_at IS NOT NULL
				AND sub.trial_threshold_notified_at < NOW() - INTERVAL '7 days'
		)`,
	)

	var cells []Cell
	err := db.
		// size sale siempre de cellSteps (constantes), nunca del cliente: seguro de interpolar.
		Group(fmt.Sprintf("FLOOR(entities.latitude / %g), FLOOR(entities.longitude / %g)", size, size)).
		Order("COUNT(*) DESC").
		Limit(MaxCells + 1).
		Scan(&cells).Error
	if err != nil {
		return nil, 0, false, err
	}
	truncated := len(cells) > MaxCells
	if truncated {
		cells = cells[:MaxCells]
	}
	return cells, size, truncated, nil
}
