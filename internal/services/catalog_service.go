package services

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Catálogo del negocio (E2): los servicios que ofrece habitualmente, con su precio habitual. Las reglas
// viven aquí y en la base de datos, no en la pantalla.

const (
	// MaxActiveServices es el tope de servicios activos por negocio: un catálogo corto se mantiene vivo.
	MaxActiveServices = 30
	maxServiceName    = 80
	maxServiceDesc    = 240
	minServiceName    = 2
	// MaxServicePrice es el tope razonable de un precio habitual (pesos).
	MaxServicePrice = 10_000_000
	// CatalogFreshDays es cuántos días un precio se considera vigente sin confirmar de nuevo.
	CatalogFreshDays = 90
)

var (
	ErrCatalogNotFound = errors.New("negocio o servicio no encontrado")
	ErrCatalogNotOwner = errors.New("no autorizado")
	ErrCatalogLimit    = errors.New("el catálogo ya tiene el máximo de servicios")
	// ErrCatalogInvalid envuelve los errores de validación de un servicio (el cliente se equivocó: 400).
	ErrCatalogInvalid = errors.New("datos inválidos")
)

func invalidService(msg string) error { return fmt.Errorf("%w: %s", ErrCatalogInvalid, msg) }

// ServiceInput es lo que el dueño envía para crear o editar un servicio.
type ServiceInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       *int   `json:"price"`
	PriceFrom   bool   `json:"price_from"`
}

// ValidateServiceInput limpia y valida un servicio. El precio es opcional; «desde» sin precio no tiene sentido y se
// descarta.
func ValidateServiceInput(in ServiceInput) (ServiceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if n := utf8.RuneCountInString(in.Name); n < minServiceName || n > maxServiceName {
		return in, invalidService("el nombre debe tener entre 2 y 80 caracteres")
	}
	if utf8.RuneCountInString(in.Description) > maxServiceDesc {
		return in, invalidService("la descripción admite hasta 240 caracteres")
	}
	if in.Price != nil && (*in.Price < 0 || *in.Price > MaxServicePrice) {
		return in, invalidService("el precio debe estar entre 0 y 10.000.000")
	}
	if in.Price == nil {
		in.PriceFrom = false
	}
	return in, nil
}

// PriceFreshness dice si el precio de un servicio se puede dar por vigente: se fijó o se confirmó hace menos de
// CatalogFreshDays días. Un precio sin fecha nunca es vigente.
func PriceFreshness(item models.ServiceItem, now time.Time) bool {
	last := item.PriceUpdatedAt
	if item.ConfirmedAt != nil && (last == nil || item.ConfirmedAt.After(*last)) {
		last = item.ConfirmedAt
	}
	if last == nil {
		return false
	}
	return now.Sub(*last) <= time.Duration(CatalogFreshDays)*24*time.Hour
}

type CatalogService struct {
	DB *gorm.DB
}

func NewCatalogService(db *gorm.DB) *CatalogService {
	return &CatalogService{DB: db}
}

// owned comprueba que el negocio existe y es de `userID`.
func (s *CatalogService) owned(entityID, userID uuid.UUID) error {
	var e models.Entity
	if err := s.DB.Select("id", "owner_id").First(&e, "id = ?", entityID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCatalogNotFound
		}
		return err
	}
	if e.OwnerID != userID {
		return ErrCatalogNotOwner
	}
	return nil
}

// List devuelve los servicios activos del negocio (lectura pública), en orden.
func (s *CatalogService) List(entityID uuid.UUID) ([]models.ServiceItem, error) {
	var items []models.ServiceItem
	err := s.DB.Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).
		Order("position ASC, created_at ASC").Find(&items).Error
	return items, err
}

// Create añade un servicio al catálogo (solo el dueño).
func (s *CatalogService) Create(entityID, userID uuid.UUID, in ServiceInput, now time.Time) (*models.ServiceItem, error) {
	if err := s.owned(entityID, userID); err != nil {
		return nil, err
	}
	in, err := ValidateServiceInput(in)
	if err != nil {
		return nil, err
	}
	var active int64
	if err := s.DB.Model(&models.ServiceItem{}).Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Count(&active).Error; err != nil {
		return nil, err
	}
	if active >= MaxActiveServices {
		return nil, ErrCatalogLimit
	}
	item := models.ServiceItem{
		EntityID: entityID, Name: in.Name, Description: in.Description,
		Price: in.Price, PriceFrom: in.PriceFrom, Status: models.ServiceActive, Position: int(active),
	}
	if in.Price != nil {
		item.PriceUpdatedAt = &now
	}
	if err := s.DB.Create(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Update edita un servicio (solo el dueño). Cambiar el precio (o «desde») actualiza la fecha del precio; cambiar
// solo el nombre o la descripción no.
func (s *CatalogService) Update(entityID, itemID, userID uuid.UUID, in ServiceInput, now time.Time) (*models.ServiceItem, error) {
	if err := s.owned(entityID, userID); err != nil {
		return nil, err
	}
	in, err := ValidateServiceInput(in)
	if err != nil {
		return nil, err
	}
	var item models.ServiceItem
	if err := s.DB.First(&item, "id = ? AND entity_id = ? AND status = ?", itemID, entityID, models.ServiceActive).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCatalogNotFound
		}
		return nil, err
	}
	priceChanged := (item.Price == nil) != (in.Price == nil) ||
		(item.Price != nil && in.Price != nil && *item.Price != *in.Price) || item.PriceFrom != in.PriceFrom
	item.Name, item.Description, item.PriceFrom = in.Name, in.Description, in.PriceFrom
	item.Price = in.Price
	if priceChanged {
		if in.Price != nil {
			item.PriceUpdatedAt = &now
		} else {
			item.PriceUpdatedAt = nil
		}
	}
	// Se guarda todo explícitamente: un precio nulo debe poder quedar nulo.
	if err := s.DB.Model(&item).Select("name", "description", "price", "price_from", "price_updated_at").Updates(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Retire marca un servicio como retirado (solo el dueño): deja de mostrarse pero se conserva el historial.
func (s *CatalogService) Retire(entityID, itemID, userID uuid.UUID) error {
	if err := s.owned(entityID, userID); err != nil {
		return err
	}
	res := s.DB.Model(&models.ServiceItem{}).
		Where("id = ? AND entity_id = ? AND status = ?", itemID, entityID, models.ServiceActive).
		Update("status", models.ServiceRetired)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCatalogNotFound
	}
	return nil
}

// Confirm marca todo el catálogo activo como vigente hoy (un solo toque del dueño). Devuelve cuántos servicios.
func (s *CatalogService) Confirm(entityID, userID uuid.UUID, now time.Time) (int64, error) {
	if err := s.owned(entityID, userID); err != nil {
		return 0, err
	}
	res := s.DB.Model(&models.ServiceItem{}).
		Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).
		Update("confirmed_at", now)
	return res.RowsAffected, res.Error
}
