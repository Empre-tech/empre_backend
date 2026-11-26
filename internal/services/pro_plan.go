package services

import (
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"empre_backend/internal/models"
)

// Empre Pro por dueño, con niveles (2026-11-26, opción A). Se compra en Google Play o en la App Store a través de
// RevenueCat: «Pro 1 negocio», «Pro 2 negocios», «Pro 3 negocios». El primer negocio vale ProMonthlyPriceCOP y cada
// uno más, la mitad. El dueño elige qué negocios cubre; un negocio cubierto por un plan vigente es Pro igual que uno
// pagado con Wompi o puesto a mano por el administrador.

// MaxProTier: hasta cuántos negocios cubre el plan más grande.
const MaxProTier = 3

// ProTierProducts: el id base de cada producto en las tiendas y el nivel que da. En Google Play el id que llega puede
// traer el plan base («empre_pro_1:mensual»); se compara solo la parte de antes de «:».
var ProTierProducts = map[string]int{
	"empre_pro_1": 1,
	"empre_pro_2": 2,
	"empre_pro_3": 3,
}

// PlusProduct: la suscripción de usuario de «Pregúntale a Empre» (paquete aparte, 9.900 COP).
const PlusProduct = "empre_plus"

// ProTierPriceCOP: el precio de referencia de un nivel (el que cobra la tienda puede variar por impuestos o redondeo).
func ProTierPriceCOP(tier int) int {
	if tier < 1 {
		return 0
	}
	return ProMonthlyPriceCOP + (tier-1)*ProExtraPriceCOP
}

func productBase(productID string) string {
	id := strings.ToLower(strings.TrimSpace(productID))
	if i := strings.Index(id, ":"); i >= 0 {
		id = id[:i]
	}
	return id
}

// productTier: el nivel de Pro de un producto, o 0 si no es de Pro.
func productTier(productID string) int {
	return ProTierProducts[productBase(productID)]
}

func isPlusProduct(productID string) bool {
	return productBase(productID) == PlusProduct
}

var (
	// ErrProCoverageTooMany: eligió más negocios de los que cubre su plan.
	ErrProCoverageTooMany = errors.New("pro coverage exceeds tier")
	// ErrProCoverageNotOwner: uno de los negocios no es suyo.
	ErrProCoverageNotOwner = errors.New("pro coverage entity not owned")
	// ErrStoreWebhookAuth: el aviso no trae el secreto configurado.
	ErrStoreWebhookAuth = errors.New("store webhook unauthorized")
)

// ownerPlanCovers: hasta cuándo es Pro un negocio por el plan de su dueño, o nil si no lo cubre un plan vigente.
func ownerPlanCovers(db *gorm.DB, entityID uuid.UUID, now time.Time) (*time.Time, error) {
	var row struct{ CurrentPeriodEnd time.Time }
	res := db.Raw(`
		SELECT p.current_period_end FROM pro_coverages c
		JOIN entities e ON e.id = c.entity_id AND e.owner_id = c.user_id AND e.deleted_at IS NULL
		JOIN owner_pro_plans p ON p.user_id = c.user_id
		WHERE c.entity_id = ? AND p.current_period_end > ?`, entityID, now).Scan(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 || row.CurrentPeriodEnd.IsZero() {
		return nil, nil
	}
	return &row.CurrentPeriodEnd, nil
}

// ProPlanService maneja el plan del dueño: los avisos de RevenueCat y qué negocios cubre.
type ProPlanService struct {
	DB *gorm.DB
	// WebhookSecret: el valor que RevenueCat manda en la cabecera Authorization (se configura en su panel).
	WebhookSecret string
}

func NewProPlanService(db *gorm.DB, webhookSecret string) *ProPlanService {
	return &ProPlanService{DB: db, WebhookSecret: strings.TrimSpace(webhookSecret)}
}

// Enabled: hay secreto para recibir los avisos.
func (s *ProPlanService) Enabled() bool { return s.WebhookSecret != "" }

// CheckWebhookAuth compara la cabecera Authorization con el secreto (acepta «Bearer <secreto>»), en tiempo constante.
func (s *ProPlanService) CheckWebhookAuth(header string) error {
	if !s.Enabled() {
		return ErrStoreWebhookAuth
	}
	got := strings.TrimSpace(header)
	got = strings.TrimSpace(strings.TrimPrefix(got, "Bearer "))
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.WebhookSecret)) != 1 {
		return ErrStoreWebhookAuth
	}
	return nil
}

// RevenueCatEvent: lo que usamos de un aviso de RevenueCat (https://www.revenuecat.com/docs/integrations/webhooks).
type RevenueCatEvent struct {
	ID                 string   `json:"id"`
	Type               string   `json:"type"`
	AppUserID          string   `json:"app_user_id"`
	OriginalAppUserID  string   `json:"original_app_user_id"`
	Aliases            []string `json:"aliases"`
	ProductID          string   `json:"product_id"`
	NewProductID       string   `json:"new_product_id"`
	ExpirationAtMs     *int64   `json:"expiration_at_ms"`
	EventTimestampMs   int64    `json:"event_timestamp_ms"`
	Store              string   `json:"store"`
	Environment        string   `json:"environment"`
	TransferredFrom    []string `json:"transferred_from"`
	TransferredTo      []string `json:"transferred_to"`
	CancelReason       string   `json:"cancel_reason"`
	PeriodType         string   `json:"period_type"`
	EntitlementIDs     []string `json:"entitlement_ids"`
	PresentedOfferings string   `json:"presented_offering_id"`
}

// userOf: el usuario de Empre del aviso. La app hace Purchases.logIn(<id del usuario>), así que es un UUID.
func (e RevenueCatEvent) userOf() (uuid.UUID, bool) {
	for _, s := range append([]string{e.AppUserID, e.OriginalAppUserID}, e.Aliases...) {
		if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

func storeName(s string) string {
	switch strings.ToUpper(s) {
	case "PLAY_STORE":
		return "play_store"
	case "APP_STORE", "MAC_APP_STORE":
		return "app_store"
	default:
		return strings.ToLower(s)
	}
}

// HandleRevenueCat aplica un aviso. Devuelve applied=false cuando no había nada que hacer (repetido, prueba, otro
// producto o un usuario que no es de Empre); RevenueCat igual recibe 200 para que no reintente.
func (s *ProPlanService) HandleRevenueCat(ev RevenueCatEvent, now time.Time) (bool, error) {
	if ev.ID == "" || ev.Type == "" || strings.EqualFold(ev.Type, "TEST") {
		return false, nil
	}
	applied := false
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// El mismo aviso una sola vez.
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.StoreEvent{ID: ev.ID, Type: ev.Type, UserID: ev.AppUserID, ProductID: ev.ProductID})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if strings.EqualFold(ev.Type, "TRANSFER") {
			// La compra pasó de una cuenta de Empre a otra (la misma cuenta de la tienda en otro usuario): el plan se va
			// con la compra. El aviso de renovación siguiente lo deja en el usuario nuevo; aquí se quita del anterior.
			for _, from := range ev.TransferredFrom {
				if id, err := uuid.Parse(from); err == nil {
					if err := tx.Model(&models.OwnerProPlan{}).Where("user_id = ?", id).
						Updates(map[string]any{"current_period_end": now, "will_renew": false}).Error; err != nil {
						return err
					}
					applied = true
				}
			}
			return nil
		}
		userID, ok := ev.userOf()
		if !ok {
			return nil
		}
		product := ev.ProductID
		if strings.EqualFold(ev.Type, "PRODUCT_CHANGE") && ev.NewProductID != "" {
			product = ev.NewProductID
		}
		var at time.Time
		if ev.EventTimestampMs > 0 {
			at = time.UnixMilli(ev.EventTimestampMs)
		} else {
			at = now
		}
		var exp *time.Time
		if ev.ExpirationAtMs != nil && *ev.ExpirationAtMs > 0 {
			t := time.UnixMilli(*ev.ExpirationAtMs)
			exp = &t
		}
		if isPlusProduct(product) {
			ok, err := applyPlus(tx, userID, ev, exp, now)
			applied = ok
			return err
		}
		tier := productTier(product)
		if tier == 0 {
			return nil
		}
		ok2, err := applyPro(tx, userID, tier, product, ev, exp, at, now)
		applied = ok2
		return err
	})
	return applied, err
}

func applyPlus(tx *gorm.DB, userID uuid.UUID, ev RevenueCatEvent, exp *time.Time, now time.Time) (bool, error) {
	switch strings.ToUpper(ev.Type) {
	case "INITIAL_PURCHASE", "RENEWAL", "UNCANCELLATION", "PRODUCT_CHANGE", "BILLING_ISSUE", "SUBSCRIPTION_EXTENDED":
		if exp == nil {
			return false, nil
		}
		plan := models.UserPlan{UserID: userID, Plan: "plus", CurrentPeriodEnd: *exp, Source: storeName(ev.Store)}
		return true, tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"plan", "current_period_end", "source", "updated_at"}),
		}).Create(&plan).Error
	case "EXPIRATION":
		end := now
		if exp != nil && exp.Before(now) {
			end = *exp
		}
		return true, tx.Model(&models.UserPlan{}).Where("user_id = ? AND source <> ?", userID, "manual").Update("current_period_end", end).Error
	}
	return false, nil
}

func applyPro(tx *gorm.DB, userID uuid.UUID, tier int, product string, ev RevenueCatEvent, exp *time.Time, at, now time.Time) (bool, error) {
	var cur models.OwnerProPlan
	found := tx.Where("user_id = ?", userID).Limit(1).Find(&cur).RowsAffected > 0
	// Un aviso viejo que llega tarde no pisa uno más nuevo.
	if found && !cur.LastEventAt.IsZero() && at.Before(cur.LastEventAt) {
		return false, nil
	}
	switch strings.ToUpper(ev.Type) {
	case "INITIAL_PURCHASE", "RENEWAL", "UNCANCELLATION", "PRODUCT_CHANGE", "BILLING_ISSUE", "SUBSCRIPTION_EXTENDED":
		if exp == nil {
			return false, nil
		}
		if strings.EqualFold(ev.Type, "PRODUCT_CHANGE") && found && tier < cur.Tier {
			// Bajar de nivel aplica al renovar (la tienda cobra el nivel nuevo desde entonces): llega con la RENEWAL.
			return false, nil
		}
		plan := models.OwnerProPlan{
			UserID: userID, Tier: tier, ProductID: product, Store: storeName(ev.Store),
			WillRenew: !strings.EqualFold(ev.Type, "BILLING_ISSUE") || cur.WillRenew, CurrentPeriodEnd: *exp, LastEventAt: at,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"tier", "product_id", "store", "will_renew", "current_period_end", "last_event_at", "updated_at"}),
		}).Create(&plan).Error; err != nil {
			return false, err
		}
		return true, fitCoverage(tx, userID, tier)
	case "CANCELLATION":
		// Canceló la renovación: sigue con Pro hasta que venza.
		if !found {
			return false, nil
		}
		return true, tx.Model(&models.OwnerProPlan{}).Where("user_id = ?", userID).
			Updates(map[string]any{"will_renew": false, "last_event_at": at}).Error
	case "EXPIRATION":
		if !found {
			return false, nil
		}
		end := now
		if exp != nil && exp.Before(now) {
			end = *exp
		}
		// La cobertura se queda: si vuelve a pagar, quedan cubiertos los mismos negocios.
		return true, tx.Model(&models.OwnerProPlan{}).Where("user_id = ?", userID).
			Updates(map[string]any{"current_period_end": end, "will_renew": false, "last_event_at": at}).Error
	}
	return false, nil
}

// ownedEntities: los negocios del dueño, del más antiguo al más nuevo.
func ownedEntities(tx *gorm.DB, userID uuid.UUID) ([]models.Entity, error) {
	var list []models.Entity
	err := tx.Select("id", "name", "created_at").Where("owner_id = ?", userID).Order("created_at ASC").Find(&list).Error
	return list, err
}

// fitCoverage ajusta qué negocios cubre el plan a su nivel: si cubre menos de los que puede, suma los negocios del
// dueño que falten (del más antiguo al más nuevo); si cubre más (bajó de nivel o cerró un negocio), quita los últimos
// que eligió. Los negocios que ya no son suyos salen siempre.
func fitCoverage(tx *gorm.DB, userID uuid.UUID, tier int) error {
	owned, err := ownedEntities(tx, userID)
	if err != nil {
		return err
	}
	ownedSet := map[uuid.UUID]bool{}
	for _, e := range owned {
		ownedSet[e.ID] = true
	}
	var cov []models.ProCoverage
	if err := tx.Where("user_id = ?", userID).Order("created_at ASC").Find(&cov).Error; err != nil {
		return err
	}
	keep := make([]uuid.UUID, 0, len(cov))
	var drop []uuid.UUID
	for _, c := range cov {
		if ownedSet[c.EntityID] && len(keep) < tier {
			keep = append(keep, c.EntityID)
		} else {
			drop = append(drop, c.EntityID)
		}
	}
	if len(drop) > 0 {
		if err := tx.Where("user_id = ? AND entity_id IN ?", userID, drop).Delete(&models.ProCoverage{}).Error; err != nil {
			return err
		}
	}
	kept := map[uuid.UUID]bool{}
	for _, id := range keep {
		kept[id] = true
	}
	for _, e := range owned {
		if len(keep) >= tier {
			break
		}
		if kept[e.ID] {
			continue
		}
		// Un negocio cubierto por OTRO dueño (lo cambió de dueño) se pasa a este.
		if err := tx.Where("entity_id = ?", e.ID).Delete(&models.ProCoverage{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.ProCoverage{EntityID: e.ID, UserID: userID}).Error; err != nil {
			return err
		}
		keep = append(keep, e.ID)
	}
	return nil
}

// ProOverview: lo que muestra la app en la pantalla de Pro.
type ProOverview struct {
	Plan *models.OwnerProPlan `json:"plan"`
	// Active: el plan está vigente.
	Active bool `json:"active"`
	// Covered: los negocios que cubre (vigente o no; si vence y renueva, vuelven a ser estos).
	Covered    []uuid.UUID         `json:"covered"`
	Businesses []ProOverviewEntity `json:"businesses"`
	// Prices: el precio de referencia de cada nivel (el real lo muestra la tienda).
	Prices map[int]int `json:"prices"`
	// Products: el id de producto de cada nivel, para pedírselo a la tienda.
	Products map[int]string `json:"products"`
}

type ProOverviewEntity struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Covered bool      `json:"covered"`
}

func proProductsByTier() map[int]string {
	out := map[int]string{}
	for id, tier := range ProTierProducts {
		out[tier] = id
	}
	return out
}

// Overview: el plan del dueño, sus negocios y cuáles cubre.
func (s *ProPlanService) Overview(userID uuid.UUID, now time.Time) (ProOverview, error) {
	out := ProOverview{Covered: []uuid.UUID{}, Businesses: []ProOverviewEntity{}, Prices: map[int]int{}, Products: proProductsByTier()}
	for t := 1; t <= MaxProTier; t++ {
		out.Prices[t] = ProTierPriceCOP(t)
	}
	var plan models.OwnerProPlan
	if s.DB.Where("user_id = ?", userID).Limit(1).Find(&plan).RowsAffected > 0 {
		out.Plan = &plan
		out.Active = plan.CurrentPeriodEnd.After(now)
	}
	owned, err := ownedEntities(s.DB, userID)
	if err != nil {
		return out, err
	}
	var cov []models.ProCoverage
	if err := s.DB.Where("user_id = ?", userID).Order("created_at ASC").Find(&cov).Error; err != nil {
		return out, err
	}
	covered := map[uuid.UUID]bool{}
	for _, c := range cov {
		covered[c.EntityID] = true
	}
	for _, e := range owned {
		out.Businesses = append(out.Businesses, ProOverviewEntity{ID: e.ID, Name: e.Name, Covered: covered[e.ID]})
		if covered[e.ID] {
			out.Covered = append(out.Covered, e.ID)
		}
	}
	return out, nil
}

// SetCoverage: el dueño elige qué negocios cubre su plan. Sin plan vigente puede dejar marcado uno (el que va a
// comprar): cuando llegue la compra, ese queda cubierto primero.
func (s *ProPlanService) SetCoverage(userID uuid.UUID, entityIDs []uuid.UUID, now time.Time) (ProOverview, error) {
	seen := map[uuid.UUID]bool{}
	ids := make([]uuid.UUID, 0, len(entityIDs))
	for _, id := range entityIDs {
		if id != uuid.Nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	limit := 1
	var plan models.OwnerProPlan
	if s.DB.Where("user_id = ?", userID).Limit(1).Find(&plan).RowsAffected > 0 && plan.CurrentPeriodEnd.After(now) {
		limit = plan.Tier
	}
	if len(ids) > limit {
		return ProOverview{}, ErrProCoverageTooMany
	}
	owned, err := ownedEntities(s.DB, userID)
	if err != nil {
		return ProOverview{}, err
	}
	ownedSet := map[uuid.UUID]bool{}
	for _, e := range owned {
		ownedSet[e.ID] = true
	}
	for _, id := range ids {
		if !ownedSet[id] {
			return ProOverview{}, ErrProCoverageNotOwner
		}
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&models.ProCoverage{}).Error; err != nil {
			return err
		}
		base := now
		for i, id := range ids {
			if err := tx.Where("entity_id = ?", id).Delete(&models.ProCoverage{}).Error; err != nil {
				return err
			}
			// El orden en que los eligió: si baja de nivel, se quitan los últimos.
			if err := tx.Create(&models.ProCoverage{EntityID: id, UserID: userID, CreatedAt: base.Add(time.Duration(i) * time.Millisecond)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ProOverview{}, err
	}
	return s.Overview(userID, now)
}
