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
	PlanProMonthly        = "pro_monthly"
	ProMonthlyPriceCOP    = 39900 // pesos colombianos
	SubscriptionPeriodDays = 30

	// Gratis hasta que el negocio reciba mensajes de este número de clientes
	// DISTINTOS (no de mensajes totales: un mismo cliente escribiendo varias
	// veces no cuenta más de una vez). Así el que paga es el que ya está
	// recibiendo demanda real, no una fecha en el calendario.
	FreeTrialCustomerThreshold = 10

	// Días de margen entre el push de "ya te están contactando, activa tu
	// plan" y que el negocio se oculte del mapa/búsqueda por no pagar. Debe
	// coincidir con el INTERVAL '7 days' hardcodeado en
	// EntityRepository.FindAll (no hay una sola fuente de verdad entre Go y
	// SQL crudo; si cambia este número, hay que cambiarlo también allá).
	TrialGracePeriodDays = 7
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

// StatusView is what the app actually needs to show: the raw subscription
// plus how close the business is to the free-trial threshold and whether it
// should be asked to pay.
type StatusView struct {
	Subscription      *models.Subscription `json:"subscription"`
	DistinctCustomers int64                `json:"distinct_customers"`
	TrialThreshold    int                  `json:"trial_threshold"`
	// true cuando el negocio ya superó el umbral gratis y no tiene un plan
	// activo — momento en el que le pedimos que pague.
	RequiresPayment bool `json:"requires_payment"`
}

// Status returns the entity's subscription, self-healing an expired "active"
// row to "inactive" so callers never have to special-case CurrentPeriodEnd,
// plus how many distinct customers have contacted it so the app can show
// progress toward the free-trial threshold.
func (s *SubscriptionService) Status(entityID uuid.UUID) (*StatusView, error) {
	sub, err := s.Repo.FindOrCreateByEntity(entityID)
	if err != nil {
		return nil, err
	}
	if sub.Status == models.SubscriptionActive && sub.CurrentPeriodEnd != nil && sub.CurrentPeriodEnd.Before(time.Now()) {
		sub.Status = models.SubscriptionInactive
	}

	var distinctCustomers int64
	if s.ChatService != nil {
		distinctCustomers, _ = s.ChatService.DistinctCustomerCount(entityID)
	}

	return &StatusView{
		Subscription:      sub,
		DistinctCustomers: distinctCustomers,
		TrialThreshold:    FreeTrialCustomerThreshold,
		RequiresPayment:   sub.Status != models.SubscriptionActive && distinctCustomers >= FreeTrialCustomerThreshold,
	}, nil
}

// CheckTrialThreshold is called right after a customer's message to a
// business is saved. If that business just crossed the free-trial threshold
// and doesn't have an active plan yet, it pushes a one-time notice to the
// owner (never repeats it — TrialThresholdNotifiedAt guards that).
func (s *SubscriptionService) CheckTrialThreshold(entityID uuid.UUID) {
	if s.ChatService == nil || s.PushService == nil || s.EntityService == nil {
		return
	}
	sub, err := s.Repo.FindOrCreateByEntity(entityID)
	if err != nil || sub.Status == models.SubscriptionActive || sub.TrialThresholdNotifiedAt != nil {
		return
	}
	count, err := s.ChatService.DistinctCustomerCount(entityID)
	if err != nil || count < FreeTrialCustomerThreshold {
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
			"%d clientes distintos ya te escribieron. Activa tu plan Pro en los próximos %d días o tu negocio se ocultará del mapa y las búsquedas.",
			count, TrialGracePeriodDays,
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

	amountInCents := int64(ProMonthlyPriceCOP) * 100
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
