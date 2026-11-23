package services

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"

	"github.com/google/uuid"
)

// Un solo plan por ahora: negocio "Pro" mensual. Cuando haya más de uno,
// esto pasa a ser una tabla, pero para el MVP un plan fijo es más simple de
// razonar y de mostrar en la app.
const (
	PlanProMonthly     = "pro_monthly"
	ProMonthlyPriceCOP = 39900 // pesos colombianos
	// ProExtraPriceCOP: con Pro ya activo en uno de sus negocios, cada negocio más del mismo dueño paga la mitad
	// (2026-11-22).
	ProExtraPriceCOP       = ProMonthlyPriceCOP / 2
	SubscriptionPeriodDays = 30

	// Base y Empre Pro (2026-11-17): el plan Base es gratis y nunca oculta el negocio. Cuando le escriben clientes
	// DISTINTOS por primera vez en este número (un mismo cliente no cuenta dos veces), Empre le cuenta una sola vez
	// al dueño qué suma Pro: es el momento en que ya hay demanda real. No hay plazo ni castigo.
	ProInviteCustomerThreshold = 10
)

type SubscriptionService struct {
	Repo            *repository.SubscriptionRepository
	ChatService     *ChatService
	PushService     *PushService
	EntityService   *EntityService
	WompiPublicKey  string
	IntegritySecret string
	EventsSecret    string
	RedirectURL     string
}

func NewSubscriptionService(
	repo *repository.SubscriptionRepository,
	chatService *ChatService,
	pushService *PushService,
	entityService *EntityService,
	publicKey, integritySecret, eventsSecret, redirectURL string,
) *SubscriptionService {
	return &SubscriptionService{
		Repo:            repo,
		ChatService:     chatService,
		PushService:     pushService,
		EntityService:   entityService,
		WompiPublicKey:  publicKey,
		IntegritySecret: integritySecret,
		EventsSecret:    eventsSecret,
		RedirectURL:     redirectURL,
	}
}

func (s *SubscriptionService) Enabled() bool {
	return s.WompiPublicKey != "" && s.IntegritySecret != ""
}

// StatusView is what the app needs to show the plan: the subscription (Pro when active and not expired; Base
// otherwise). There is no trial and nothing to pay to stay visible.
type StatusView struct {
	Subscription *models.Subscription `json:"subscription"`
	// PriceCOP: lo que paga este negocio por un mes de Pro. ExtraBusiness: va a mitad de precio porque el dueño ya
	// tiene Pro en otro de sus negocios.
	PriceCOP      int  `json:"price_cop"`
	ExtraBusiness bool `json:"extra_business"`
}

// PriceFor: el precio de un mes de Pro para este negocio. La mitad si el dueño ya tiene Pro en otro negocio.
func (s *SubscriptionService) PriceFor(entityID uuid.UUID) (int, bool, error) {
	extra, err := s.Repo.OwnerHasOtherPro(entityID)
	if err != nil {
		return ProMonthlyPriceCOP, false, err
	}
	if extra {
		return ProExtraPriceCOP, true, nil
	}
	return ProMonthlyPriceCOP, false, nil
}

// Status returns the entity's subscription, self-healing an expired "active"
// row to "inactive" so callers never have to special-case CurrentPeriodEnd.
func (s *SubscriptionService) Status(entityID uuid.UUID) (*StatusView, error) {
	sub, err := s.Repo.FindOrCreateByEntity(entityID)
	if err != nil {
		return nil, err
	}
	if sub.Status == models.SubscriptionActive && sub.CurrentPeriodEnd != nil && sub.CurrentPeriodEnd.Before(time.Now()) {
		sub.Status = models.SubscriptionInactive
	}

	price, extra, err := s.PriceFor(entityID)
	if err != nil {
		return nil, err
	}
	return &StatusView{Subscription: sub, PriceCOP: price, ExtraBusiness: extra}, nil
}

// CheckProInvite is called right after a customer's message to a business is saved. When a Base business reaches
// ProInviteCustomerThreshold distinct customers, the owner gets one notice about what Pro adds (never repeated:
// TrialThresholdNotifiedAt, a column kept from the old trial, guards that). It never hides the business.
func (s *SubscriptionService) CheckProInvite(entityID uuid.UUID) {
	if s.ChatService == nil || s.PushService == nil || s.EntityService == nil {
		return
	}
	sub, err := s.Repo.FindOrCreateByEntity(entityID)
	if err != nil || sub.Status == models.SubscriptionActive || sub.TrialThresholdNotifiedAt != nil {
		return
	}
	count, err := s.ChatService.DistinctCustomerCount(entityID)
	if err != nil || count < ProInviteCustomerThreshold {
		return
	}
	entity, err := s.EntityService.FindByID(entityID)
	if err != nil {
		return
	}
	now := time.Now()
	if err := s.Repo.MarkTrialThresholdNotified(sub.ID, now); err != nil {
		return
	}
	s.PushService.Notify(
		entity.OwnerID,
		"¡Ya te están contactando en Empre!",
		fmt.Sprintf(
			"%d clientes distintos ya te escribieron. Tu plan Base sigue gratis; con Empre Pro anotas con tu voz y las citas se apartan solas.",
			count,
		),
		map[string]string{"type": "subscription", "entity_id": entityID.String()},
	)
}

// CheckoutData is everything the app needs to open Wompi's hosted Web
// Checkout widget for this payment attempt.
type CheckoutData struct {
	PublicKey     string `json:"public_key"`
	Reference     string `json:"reference"`
	AmountInCents int64  `json:"amount_in_cents"`
	Currency      string `json:"currency"`
	Signature     string `json:"signature"`
	RedirectURL   string `json:"redirect_url"`
}

// CreateCheckout starts a new payment attempt: it records a PENDING row we
// can later match against the Wompi webhook by its reference, and returns
// the integrity-signed data the widget needs (Wompi requires this signature
// so the amount/reference can't be tampered with client-side).
func (s *SubscriptionService) CreateCheckout(entityID uuid.UUID) (*CheckoutData, error) {
	if !s.Enabled() {
		return nil, errors.New("los pagos no están configurados todavía")
	}
	sub, err := s.Repo.FindOrCreateByEntity(entityID)
	if err != nil {
		return nil, err
	}

	price, _, err := s.PriceFor(entityID)
	if err != nil {
		return nil, err
	}
	amountInCents := int64(price) * 100
	reference := fmt.Sprintf("empre-%s-%d", entityID.String()[:8], time.Now().UnixNano())

	payment := &models.SubscriptionPayment{
		SubscriptionID: sub.ID,
		EntityID:       entityID,
		Reference:      reference,
		AmountInCents:  amountInCents,
		Currency:       "COP",
		Status:         models.PaymentPending,
	}
	if err := s.Repo.CreatePayment(payment); err != nil {
		return nil, err
	}

	signature := s.integritySignature(reference, amountInCents, "COP")

	return &CheckoutData{
		PublicKey:     s.WompiPublicKey,
		Reference:     reference,
		AmountInCents: amountInCents,
		Currency:      "COP",
		Signature:     signature,
		RedirectURL:   s.RedirectURL,
	}, nil
}

// integritySignature implements Wompi's Web Checkout integrity signature:
// SHA256("<Reference><AmountInCents><Currency><IntegritySecret>"), hex.
// https://docs.wompi.co/ (Widget Checkout Web — firma de integridad)
func (s *SubscriptionService) integritySignature(reference string, amountInCents int64, currency string) string {
	raw := reference + strconv.FormatInt(amountInCents, 10) + currency + s.IntegritySecret
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// VerifyEventSignature checks a Wompi webhook's checksum against the events
// secret, per https://docs.wompi.co/ (Eventos — verificación de la firma).
// `properties` is event.signature.properties (dot-paths into event.data),
// `values` are those same paths already resolved by the caller in order.
func (s *SubscriptionService) VerifyEventSignature(values []string, timestamp int64, checksum string) bool {
	if s.EventsSecret == "" {
		return false
	}
	raw := strings.Join(values, "") + strconv.FormatInt(timestamp, 10) + s.EventsSecret
	sum := sha256.Sum256([]byte(raw))
	computed := hex.EncodeToString(sum[:])
	return strings.EqualFold(computed, checksum)
}

// ApplyApprovedPayment activates (or extends) a subscription once a payment
// attempt is confirmed APPROVED. Paying while still active extends from the
// current expiry instead of from now, so renewing early never wastes days.
func (s *SubscriptionService) ApplyApprovedPayment(reference, wompiTransactionID string) error {
	payment, err := s.Repo.FindPaymentByReference(reference)
	if err != nil {
		return err
	}
	if payment.Status == models.PaymentApproved {
		return nil // evento duplicado (Wompi puede reenviar); ya se aplicó.
	}
	if err := s.Repo.UpdatePaymentStatus(payment.ID, models.PaymentApproved, wompiTransactionID); err != nil {
		return err
	}

	sub, err := s.Repo.FindSubscriptionByID(payment.SubscriptionID)
	if err != nil {
		return err
	}
	base := time.Now()
	if sub.Status == models.SubscriptionActive && sub.CurrentPeriodEnd != nil && sub.CurrentPeriodEnd.After(base) {
		base = *sub.CurrentPeriodEnd
	}
	periodEnd := base.AddDate(0, 0, SubscriptionPeriodDays)
	return s.Repo.ActivateSubscription(sub.ID, periodEnd)
}

// MarkPaymentStatus records a non-approved final status (DECLINED, ERROR,
// VOIDED) reported by the webhook, without touching the subscription.
func (s *SubscriptionService) MarkPaymentStatus(reference string, status models.PaymentStatus, wompiTransactionID string) error {
	payment, err := s.Repo.FindPaymentByReference(reference)
	if err != nil {
		return err
	}
	return s.Repo.UpdatePaymentStatus(payment.ID, status, wompiTransactionID)
}
