package repository

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SubscriptionRepository struct {
	DB *gorm.DB
}

func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{DB: db}
}

// FindOrCreateByEntity returns the entity's subscription row, creating an
// "inactive" one on first touch so callers always have something to read.
func (r *SubscriptionRepository) FindOrCreateByEntity(entityID uuid.UUID) (*models.Subscription, error) {
	var sub models.Subscription
	err := r.DB.Where("entity_id = ?", entityID).First(&sub).Error
	if err == nil {
		return &sub, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	sub = models.Subscription{EntityID: entityID, Status: models.SubscriptionInactive}
	if err := r.DB.Create(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// CreatePayment records a new checkout attempt (PENDING) for a subscription.
func (r *SubscriptionRepository) CreatePayment(payment *models.SubscriptionPayment) error {
	return r.DB.Create(payment).Error
}

// FindPaymentByReference looks up a checkout attempt by the reference we
// generated for it — the only handle the Wompi webhook gives us back.
func (r *SubscriptionRepository) FindPaymentByReference(reference string) (*models.SubscriptionPayment, error) {
	var payment models.SubscriptionPayment
	if err := r.DB.Where("reference = ?", reference).First(&payment).Error; err != nil {
		return nil, err
	}
	return &payment, nil
}

// FindSubscriptionByID loads a subscription by its own id (used once we have
// a payment's SubscriptionID).
func (r *SubscriptionRepository) FindSubscriptionByID(id uuid.UUID) (*models.Subscription, error) {
	var sub models.Subscription
	if err := r.DB.Where("id = ?", id).First(&sub).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// UpdatePaymentStatus stamps the final status a webhook reported for a
// checkout attempt, plus Wompi's own transaction id for traceability.
func (r *SubscriptionRepository) UpdatePaymentStatus(paymentID uuid.UUID, status models.PaymentStatus, wompiTransactionID string) error {
	return r.DB.Model(&models.SubscriptionPayment{}).Where("id = ?", paymentID).Updates(map[string]interface{}{
		"status":               status,
		"wompi_transaction_id": wompiTransactionID,
	}).Error
}

// MarkTrialThresholdNotified stamps that we already pushed the "your free
// trial is over, activate your plan" notice, so we never send it twice.
func (r *SubscriptionRepository) MarkTrialThresholdNotified(subscriptionID uuid.UUID, when time.Time) error {
	return r.DB.Model(&models.Subscription{}).Where("id = ?", subscriptionID).Update("trial_threshold_notified_at", when).Error
}

// ActivateSubscription marks a subscription active through periodEnd. If it
// was already active and not yet expired, callers should pass a periodEnd
// that already accounts for the extension (now or current expiry, whichever
// is later, plus one billing cycle) so paying early doesn't waste days.
func (r *SubscriptionRepository) ActivateSubscription(subscriptionID uuid.UUID, periodEnd time.Time) error {
	return r.DB.Model(&models.Subscription{}).Where("id = ?", subscriptionID).Updates(map[string]interface{}{
		"status":                       models.SubscriptionActive,
		"current_period_end":           periodEnd,
		// Se limpia el aviso de "superaste el umbral gratis": si este plan
		// vuelve a vencer en el futuro sin renovarse, el negocio debe recibir
		// un aviso nuevo y un margen de gracia nuevo antes de ocultarse otra
		// vez, no heredar el de esta vez.
		"trial_threshold_notified_at": nil,
	}).Error
}
