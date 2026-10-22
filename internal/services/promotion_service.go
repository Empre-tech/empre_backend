package services

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// maxActivePromotions es el límite de promociones activas por negocio (decisión).
const maxActivePromotions = 3

const (
	// MaxPromotionItems es el tope de servicios que destaca una promoción.
	MaxPromotionItems = 5
	maxPromoTitle     = 80
	maxPromoMessage   = 240
	maxPromoMotive    = 200
)

var (
	ErrPromotionNotFound = errors.New("promoción no encontrada")
	ErrPromotionNotOwner = errors.New("no autorizado para este negocio")
	// ErrPromotionInvalid envuelve los errores de validación del contenido (el cliente se equivocó).
	ErrPromotionInvalid = errors.New("datos inválidos")
)

func invalidPromotion(msg string) error { return fmt.Errorf("%w: %s", ErrPromotionInvalid, msg) }

type PromotionService struct {
	PromotionRepo *repository.PromotionRepository
	EntityRepo    *repository.EntityRepository
}

func NewPromotionService(promotionRepo *repository.PromotionRepository, entityRepo *repository.EntityRepository) *PromotionService {
	return &PromotionService{PromotionRepo: promotionRepo, EntityRepo: entityRepo}
}

type PublishPromotionInput struct {
	Title       string
	Description string
	Price       string
	Discount    string
	NormalPrice string
	StartsAt    time.Time
	EndsAt      time.Time
	Conditions  string
	Tags        string

	// Editor por plantillas. MotiveInternal es privado (solo el dueño lo ve); PublicMessage es lo que ve el cliente.
	MotiveInternal string
	PublicMessage  string
	Template       models.PromotionTemplate
	PhotoMediaID   *uuid.UUID
	// ServiceItemIDs son los servicios del catálogo que destaca. En una edición solo se tocan si ReplaceItems es true
	// (así «no enviarlos» no significa «quitarlos»).
	ServiceItemIDs []uuid.UUID
	ReplaceItems   bool
	// Draft crea la promoción como borrador (no pública, no cuenta para el límite, sin exigir fecha de fin).
	Draft bool
}

// PromotionContent es lo que se valida de una promoción, sin tocar la base de datos.
type PromotionContent struct {
	Title          string
	PublicMessage  string
	MotiveInternal string
	Template       models.PromotionTemplate
	Price          string
	Discount       string
	ItemCount      int
}

// ValidatePromotionContent revisa largos, plantilla y lo que cada plantilla exige: una promoción sin plantilla (la
// de antes del editor) pide precio o descuento; «servicio estrella» pide un servicio y «paquete» dos o más. La
// celebración no pide descuento: comunicar no es rebajar.
func ValidatePromotionContent(c PromotionContent) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(c.Title)); n < 2 || n > maxPromoTitle {
		return invalidPromotion("el título debe tener entre 2 y 80 caracteres")
	}
	if utf8.RuneCountInString(c.PublicMessage) > maxPromoMessage {
		return invalidPromotion("el mensaje para clientes admite hasta 240 caracteres")
	}
	if utf8.RuneCountInString(c.MotiveInternal) > maxPromoMotive {
		return invalidPromotion("el motivo interno admite hasta 200 caracteres")
	}
	if !c.Template.Valid() {
		return invalidPromotion("plantilla inválida")
	}
	switch c.Template {
	case models.TemplateNone:
		if c.Price == "" && c.Discount == "" {
			return invalidPromotion("indica un precio o un descuento")
		}
	case models.TemplateEstrella:
		if c.ItemCount < 1 {
			return invalidPromotion("elige el servicio que quieres destacar")
		}
	case models.TemplatePaquete:
		if c.ItemCount < 2 {
			return invalidPromotion("un paquete incluye al menos dos servicios")
		}
	}
	return nil
}

func (s *PromotionService) ensureOwner(entityID, ownerID uuid.UUID) (*models.Entity, error) {
	entity, err := s.EntityRepo.FindByID(entityID)
	if err != nil {
		return nil, errors.New("negocio no encontrado")
	}
	if entity.OwnerID != ownerID {
		return nil, ErrPromotionNotOwner
	}
	return entity, nil
}

// checkCover comprueba que la imagen de portada existe y es una imagen (JPEG, PNG o WebP): una promoción nunca queda con
// una portada que no carga ni con un archivo que no es una imagen. Nil = sin portada.
func (s *PromotionService) checkCover(id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	var m models.Media
	if err := s.PromotionRepo.DB.Select("id", "content_type").First(&m, "id = ?", *id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidPromotion("la imagen de portada no existe: vuelve a subirla")
		}
		return err
	}
	switch m.ContentType {
	case "image/jpeg", "image/png", "image/webp":
		return nil
	}
	return invalidPromotion("la portada debe ser una imagen JPEG, PNG o WebP")
}

// freeze copia los servicios pedidos (activos, del mismo negocio) como elementos congelados de la promoción.
func freezePromotionItems(db *gorm.DB, entityID uuid.UUID, ids []uuid.UUID) ([]models.PromotionItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	svcs, err := LoadActiveServices(db, entityID, ids, MaxPromotionItems)
	if err != nil {
		return nil, invalidPromotion(err.Error())
	}
	items := make([]models.PromotionItem, 0, len(svcs))
	for i, sv := range svcs {
		sid := sv.ID
		items = append(items, models.PromotionItem{ServiceItemID: &sid, Name: sv.Name, Price: sv.Price, Position: i})
	}
	return items, nil
}

// Publish crea una promoción: activa, o borrador si `in.Draft`.
func (s *PromotionService) Publish(entityID, ownerID uuid.UUID, in PublishPromotionInput) (*models.Promotion, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	items, err := freezePromotionItems(s.PromotionRepo.DB, entityID, in.ServiceItemIDs)
	if err != nil {
		return nil, err
	}
	if err := ValidatePromotionContent(PromotionContent{
		Title: in.Title, PublicMessage: in.PublicMessage, MotiveInternal: in.MotiveInternal, Template: in.Template,
		Price: in.Price, Discount: in.Discount, ItemCount: len(items),
	}); err != nil {
		return nil, err
	}
	if err := s.checkCover(in.PhotoMediaID); err != nil {
		return nil, err
	}

	now := utils.NowInCartagena()
	status := models.PromotionActive
	if in.Draft {
		status = models.PromotionDraft
	} else {
		if !in.EndsAt.After(now) {
			return nil, invalidPromotion("la fecha de fin debe ser futura")
		}
		count, err := s.PromotionRepo.CountActiveByEntity(entityID, now)
		if err != nil {
			return nil, err
		}
		if count >= maxActivePromotions {
			return nil, errors.New("límite de promociones activas alcanzado")
		}
	}

	startsAt := in.StartsAt
	if startsAt.IsZero() {
		startsAt = now
	}
	p := &models.Promotion{
		ID:             uuid.New(),
		EntityID:       entityID,
		Title:          strings.TrimSpace(in.Title),
		Description:    in.Description,
		MotiveInternal: strings.TrimSpace(in.MotiveInternal),
		PublicMessage:  strings.TrimSpace(in.PublicMessage),
		Template:       in.Template,
		Price:          in.Price,
		Discount:       in.Discount,
		NormalPrice:    in.NormalPrice,
		StartsAt:       startsAt,
		EndsAt:         in.EndsAt,
		Conditions:     in.Conditions,
		Tags:           in.Tags,
		PhotoMediaID:   in.PhotoMediaID,
		Status:         status,
		Items:          items,
	}
	if err := s.PromotionRepo.Create(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Update edita campos y/o cambia el estado. Reglas del ciclo de vida: un borrador se publica (pasa a activa) y ahí se
// valida todo y se vuelven a congelar los servicios con el precio de hoy; activa y pausada se alternan; finalizada es
// definitiva.
func (s *PromotionService) Update(promotionID, ownerID uuid.UUID, in PublishPromotionInput, status string) (*models.Promotion, error) {
	p, err := s.PromotionRepo.FindByID(promotionID)
	if err != nil {
		return nil, ErrPromotionNotFound
	}
	if _, err := s.ensureOwner(p.EntityID, ownerID); err != nil {
		return nil, err
	}
	if p.Status == models.PromotionFinished {
		return nil, invalidPromotion("una promoción finalizada ya no se puede cambiar")
	}
	// La portada nueva se valida antes de tocar nada: un cambio rechazado deja la anterior.
	if err := s.checkCover(in.PhotoMediaID); err != nil {
		return nil, err
	}

	if in.Title != "" {
		p.Title = strings.TrimSpace(in.Title)
	}
	if in.Description != "" {
		p.Description = in.Description
	}
	if in.PublicMessage != "" {
		p.PublicMessage = strings.TrimSpace(in.PublicMessage)
	}
	if in.MotiveInternal != "" {
		p.MotiveInternal = strings.TrimSpace(in.MotiveInternal)
	}
	if in.Template != models.TemplateNone {
		p.Template = in.Template
	}
	if in.PhotoMediaID != nil {
		p.PhotoMediaID = in.PhotoMediaID
	}
	if in.Price != "" || in.Discount != "" {
		p.Price = in.Price
		p.Discount = in.Discount
	}
	if in.NormalPrice != "" {
		p.NormalPrice = in.NormalPrice
	}
	if !in.EndsAt.IsZero() {
		p.EndsAt = in.EndsAt
	}
	if in.Conditions != "" {
		p.Conditions = in.Conditions
	}
	if in.Tags != "" {
		p.Tags = in.Tags
	}

	next := p.Status
	if status != "" {
		if !models.PromotionStatus(status).Valid() {
			return nil, invalidPromotion("estado inválido")
		}
		next = models.PromotionStatus(status)
		if next == models.PromotionDraft && p.Status != models.PromotionDraft {
			return nil, invalidPromotion("una promoción publicada no vuelve a borrador")
		}
	}

	// Servicios: se reemplazan solo si se pidió; al publicar un borrador se vuelven a congelar los de ahora.
	var newItems []models.PromotionItem
	replace := in.ReplaceItems
	ids := in.ServiceItemIDs
	activating := next == models.PromotionActive && p.Status != models.PromotionActive
	if !replace && activating {
		var cur []models.PromotionItem
		if err := s.PromotionRepo.DB.Where("promotion_id = ? AND service_item_id IS NOT NULL", p.ID).Order("position ASC").Find(&cur).Error; err != nil {
			return nil, err
		}
		for _, it := range cur {
			ids = append(ids, *it.ServiceItemID)
		}
		replace = len(ids) > 0
	}
	if replace {
		newItems, err = freezePromotionItems(s.PromotionRepo.DB, p.EntityID, ids)
		if err != nil {
			return nil, err
		}
	}
	itemCount := len(newItems)
	if !replace {
		var n int64
		if err := s.PromotionRepo.DB.Model(&models.PromotionItem{}).Where("promotion_id = ?", p.ID).Count(&n).Error; err != nil {
			return nil, err
		}
		itemCount = int(n)
	}
	if err := ValidatePromotionContent(PromotionContent{
		Title: p.Title, PublicMessage: p.PublicMessage, MotiveInternal: p.MotiveInternal, Template: p.Template,
		Price: p.Price, Discount: p.Discount, ItemCount: itemCount,
	}); err != nil {
		return nil, err
	}

	if activating {
		now := utils.NowInCartagena()
		if !p.EndsAt.After(now) {
			return nil, invalidPromotion("la fecha de fin debe ser futura")
		}
		count, err := s.PromotionRepo.CountActiveByEntity(p.EntityID, now)
		if err != nil {
			return nil, err
		}
		if count >= maxActivePromotions {
			return nil, errors.New("límite de promociones activas alcanzado")
		}
		if p.StartsAt.After(now) || p.Status == models.PromotionDraft {
			p.StartsAt = now
		}
	}
	p.Status = next

	err = s.PromotionRepo.DB.Transaction(func(tx *gorm.DB) error {
		if replace {
			if err := tx.Where("promotion_id = ?", p.ID).Delete(&models.PromotionItem{}).Error; err != nil {
				return err
			}
			for i := range newItems {
				newItems[i].PromotionID = p.ID
			}
			if len(newItems) > 0 {
				if err := tx.Create(&newItems).Error; err != nil {
					return err
				}
			}
		}
		return tx.Omit("Items").Save(p).Error
	})
	if err != nil {
		return nil, err
	}
	if err := s.PromotionRepo.DB.Where("promotion_id = ?", p.ID).Order("position ASC, created_at ASC").Find(&p.Items).Error; err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PromotionService) ListByEntity(entityID, ownerID uuid.UUID) ([]models.Promotion, error) {
	if _, err := s.ensureOwner(entityID, ownerID); err != nil {
		return nil, err
	}
	return s.PromotionRepo.FindByEntity(entityID)
}

// ActiveForEntity devuelve las promociones activas de un negocio (perfil público).
func (s *PromotionService) ActiveForEntity(entityID uuid.UUID) ([]models.Promotion, error) {
	return s.PromotionRepo.FindActiveByEntity(entityID, utils.NowInCartagena())
}
