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
	maxServiceSection = 40
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
	// Section es opcional; vacía, el servicio va sin sección.
	Section string `json:"section"`
	// DurationMin y Bookable son opcionales: nil deja lo que había (un editor que no los conoce no los borra).
	// DurationMin 0 quita la duración y, con ella, que se pueda reservar.
	DurationMin *int  `json:"duration_min"`
	Bookable    *bool `json:"bookable"`
	// Kind: service o product. Al crear, vacío = el más probable según el tipo de negocio. Al editar se ignora: el
	// cambio de tipo tiene su propia operación (SetKind), porque mueve el inventario.
	Kind string `json:"kind"`
	// ParentID + VariantLabel crean una variante (sabor, talla) de un producto. El nombre completo lo arma el servidor.
	ParentID     *uuid.UUID `json:"parent_id"`
	VariantLabel string     `json:"variant_label"`
}

// ValidateServiceInput limpia y valida un servicio. El precio es opcional; «desde» sin precio no tiene sentido y se
// descarta.
func ValidateServiceInput(in ServiceInput) (ServiceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	// La sección se limpia de espacios repetidos para que «Bebidas» y « Bebidas » agrupen juntas.
	in.Section = strings.Join(strings.Fields(in.Section), " ")
	if utf8.RuneCountInString(in.Section) > maxServiceSection {
		return in, invalidService("la sección admite hasta 40 caracteres")
	}
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
	if in.DurationMin != nil && *in.DurationMin != 0 && (*in.DurationMin < 5 || *in.DurationMin > 480) {
		return in, invalidService("la duración debe estar entre 5 y 480 minutos")
	}
	return in, nil
}

// applySchedule pone la duración y «se puede reservar» en el ítem. Reservable exige duración.
func applySchedule(item *models.ServiceItem, in ServiceInput) error {
	if in.DurationMin != nil {
		if *in.DurationMin == 0 {
			item.DurationMin = nil
			item.Bookable = false
		} else {
			d := *in.DurationMin
			item.DurationMin = &d
		}
	}
	if in.Bookable != nil {
		item.Bookable = *in.Bookable
	}
	if item.Bookable && item.DurationMin == nil {
		return invalidService("para reservar, el servicio necesita duración")
	}
	return nil
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
	var parent *models.ServiceItem
	if in.ParentID != nil {
		p, label, err := s.variantParent(entityID, *in.ParentID, in.VariantLabel)
		if err != nil {
			return nil, err
		}
		parent = p
		in.Name, in.VariantLabel, in.Kind = p.Name+VariantSep+label, label, models.ItemProduct
		if in.Section == "" {
			in.Section = p.Section
		}
		if in.Description == "" {
			in.Description = p.Description
		}
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
	kind := in.Kind
	if kind == "" {
		kind = s.defaultItemKind(entityID)
	}
	if !models.ValidItemKind(kind) {
		return nil, invalidService("el tipo debe ser servicio o producto")
	}
	if kind == models.ItemProduct && ((in.DurationMin != nil && *in.DurationMin > 0) || (in.Bookable != nil && *in.Bookable)) {
		return nil, invalidService("un producto no lleva duración ni reserva")
	}
	item := models.ServiceItem{
		EntityID: entityID, Name: in.Name, Description: in.Description, Section: in.Section,
		Price: in.Price, PriceFrom: in.PriceFrom, Status: models.ServiceActive, Position: int(active),
		Kind: kind, KindChecked: true,
	}
	if parent != nil {
		item.ParentID, item.VariantLabel = &parent.ID, in.VariantLabel
	}
	if in.Price != nil {
		item.PriceUpdatedAt = &now
	}
	if err := applySchedule(&item, in); err != nil {
		return nil, err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		if parent == nil {
			return nil
		}
		// El producto ahora agrupa: deja de contarse (su existencia se archiva, no se borra) y se ve agotado solo si
		// lo están todas sus variantes.
		if err := tx.Model(&models.StockItem{}).Where("entity_id = ? AND service_item_id = ?", entityID, parent.ID).Update("archived", true).Error; err != nil {
			return err
		}
		return syncParentSoldOut(tx, parent.ID)
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// variantParent valida el producto al que se le agrega una variante y limpia el nombre de la variante.
func (s *CatalogService) variantParent(entityID, parentID uuid.UUID, label string) (*models.ServiceItem, string, error) {
	label = strings.Join(strings.Fields(label), " ")
	if n := utf8.RuneCountInString(label); n < 1 || n > 40 {
		return nil, "", invalidService("escribe la variante (por ejemplo, Chocolate o Talla M), de hasta 40 letras")
	}
	var p models.ServiceItem
	if err := s.DB.First(&p, "id = ? AND entity_id = ? AND status = ?", parentID, entityID, models.ServiceActive).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", ErrCatalogNotFound
		}
		return nil, "", err
	}
	if p.ParentID != nil {
		return nil, "", invalidService("una variante no puede tener variantes")
	}
	if p.IsService() {
		return nil, "", invalidService("solo un producto puede tener variantes")
	}
	var n int64
	if err := s.DB.Model(&models.ServiceItem{}).Where("parent_id = ? AND status = ?", p.ID, models.ServiceActive).Count(&n).Error; err != nil {
		return nil, "", err
	}
	if n >= MaxVariants {
		return nil, "", invalidService("un producto puede tener hasta 30 variantes")
	}
	return &p, label, nil
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
	if item.ParentID != nil {
		// Una variante: se edita su parte corta; el nombre completo se arma con el del producto.
		_, label, err := s.variantParent(entityID, *item.ParentID, firstNonEmpty(in.VariantLabel, item.VariantLabel))
		if err != nil {
			return nil, err
		}
		var p models.ServiceItem
		if err := s.DB.Select("id", "name").First(&p, "id = ?", *item.ParentID).Error; err != nil {
			return nil, err
		}
		item.VariantLabel, in.Name = label, p.Name+VariantSep+label
	}
	renamed := item.Name != in.Name || item.Section != in.Section
	priceChanged := (item.Price == nil) != (in.Price == nil) ||
		(item.Price != nil && in.Price != nil && *item.Price != *in.Price) || item.PriceFrom != in.PriceFrom
	item.Name, item.Description, item.Section, item.PriceFrom = in.Name, in.Description, in.Section, in.PriceFrom
	item.Price = in.Price
	if priceChanged {
		if in.Price != nil {
			item.PriceUpdatedAt = &now
		} else {
			item.PriceUpdatedAt = nil
		}
	}
	if item.Kind == models.ItemProduct && ((in.DurationMin != nil && *in.DurationMin > 0) || (in.Bookable != nil && *in.Bookable)) {
		return nil, invalidService("un producto no lleva duración ni reserva")
	}
	if err := applySchedule(&item, in); err != nil {
		return nil, err
	}
	// Se guarda todo explícitamente: un precio o una duración nulos deben poder quedar nulos.
	if err := s.DB.Model(&item).Select("name", "description", "section", "price", "price_from", "price_updated_at", "duration_min", "bookable", "variant_label").Updates(&item).Error; err != nil {
		return nil, err
	}
	if renamed && item.ParentID == nil {
		// Sus variantes siguen el nombre y la sección del producto.
		if err := s.DB.Exec(`UPDATE service_items SET name = ? || variant_label, section = ? WHERE parent_id = ? AND status = 'active'`,
			item.Name+VariantSep, item.Section, item.ID).Error; err != nil {
			return nil, err
		}
	}
	return &item, nil
}

// defaultItemKind: lo más probable que agregue un negocio según su tipo (una peluquería, servicios; una tienda o un
// restaurante, productos). El dueño lo cambia al agregar.
func (s *CatalogService) defaultItemKind(entityID uuid.UUID) string {
	var e models.Entity
	if err := s.DB.Select("id", "business_profile").First(&e, "id = ?", entityID).Error; err == nil && e.BusinessProfile == "service" {
		return models.ItemService
	}
	return models.ItemProduct
}

// SetKind cambia un ítem de producto a servicio o al revés (solo el dueño). Nada se borra:
//   - producto → servicio: su inventario se archiva (deja de contarse) y deja de verse agotado;
//   - servicio → producto: si tenía inventario archivado, vuelve con su historia; pierde duración y reserva.
//
// Las ventas pasadas no cambian.
func (s *CatalogService) SetKind(entityID, itemID, userID uuid.UUID, kind string) (*models.ServiceItem, error) {
	if err := s.owned(entityID, userID); err != nil {
		return nil, err
	}
	if !models.ValidItemKind(kind) {
		return nil, invalidService("el tipo debe ser servicio o producto")
	}
	var item models.ServiceItem
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&item, "id = ? AND entity_id = ? AND status = ?", itemID, entityID, models.ServiceActive).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCatalogNotFound
			}
			return err
		}
		updates := map[string]any{"kind": kind, "kind_checked": true}
		if kind == models.ItemService {
			updates["sold_out"] = false
			if err := tx.Model(&models.StockItem{}).Where("entity_id = ? AND service_item_id = ?", entityID, itemID).Update("archived", true).Error; err != nil {
				return err
			}
		} else {
			updates["duration_min"] = nil
			updates["bookable"] = false
			if err := tx.Model(&models.StockItem{}).Where("entity_id = ? AND service_item_id = ?", entityID, itemID).Update("archived", false).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&item, "id = ?", itemID).Error
	})
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ConfirmKinds da por revisado el tipo de todos los ítems del negocio (el aviso «revisa qué es cada cosa» se va).
func (s *CatalogService) ConfirmKinds(entityID, userID uuid.UUID) error {
	if err := s.owned(entityID, userID); err != nil {
		return err
	}
	return s.DB.Model(&models.ServiceItem{}).Where("entity_id = ? AND status = ?", entityID, models.ServiceActive).Update("kind_checked", true).Error
}

// Retire marca un servicio como retirado (solo el dueño): deja de mostrarse pero se conserva el historial.
func (s *CatalogService) Retire(entityID, itemID, userID uuid.UUID) error {
	if err := s.owned(entityID, userID); err != nil {
		return err
	}
	var item models.ServiceItem
	if err := s.DB.Select("id", "parent_id").First(&item, "id = ? AND entity_id = ? AND status = ?", itemID, entityID, models.ServiceActive).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCatalogNotFound
		}
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		// Quitar un producto quita también sus variantes.
		if err := tx.Model(&models.ServiceItem{}).Where("(id = ? OR parent_id = ?) AND entity_id = ? AND status = ?", itemID, itemID, entityID, models.ServiceActive).
			Update("status", models.ServiceRetired).Error; err != nil {
			return err
		}
		if item.ParentID != nil {
			return syncParentSoldOut(tx, *item.ParentID)
		}
		return nil
	})
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

// LoadActiveServices carga los servicios ACTIVOS de un negocio por id, sin repetidos y en el orden pedido, hasta
// `max`. Falla si alguno no existe, no es de ese negocio o ya fue retirado: así una oferta o promoción nunca incluye un
// servicio ajeno ni uno que el negocio ya no presta.
func LoadActiveServices(db *gorm.DB, entityID uuid.UUID, ids []uuid.UUID, max int) ([]models.ServiceItem, error) {
	seen := map[uuid.UUID]bool{}
	unique := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	if len(unique) > max {
		return nil, fmt.Errorf("se admiten hasta %d servicios", max)
	}
	if len(unique) == 0 {
		return nil, nil
	}
	var found []models.ServiceItem
	if err := db.Where("id IN ? AND entity_id = ? AND status = ?", unique, entityID, models.ServiceActive).Find(&found).Error; err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]models.ServiceItem, len(found))
	for _, f := range found {
		byID[f.ID] = f
	}
	out := make([]models.ServiceItem, 0, len(unique))
	for _, id := range unique {
		sv, ok := byID[id]
		if !ok {
			return nil, errors.New("un servicio no está en tu catálogo o ya fue retirado")
		}
		out = append(out, sv)
	}
	return out, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
