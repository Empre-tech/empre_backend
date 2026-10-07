package repository

import (
	"time"

	"empre_backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ClaimRepository struct {
	DB *gorm.DB
}

func NewClaimRepository(db *gorm.DB) *ClaimRepository {
	return &ClaimRepository{DB: db}
}

func (r *ClaimRepository) Create(claim *models.OfferClaim) error {
	return r.DB.Create(claim).Error
}

func (r *ClaimRepository) FindByID(id uuid.UUID) (*models.OfferClaim, error) {
	var claim models.OfferClaim
	err := r.DB.First(&claim, "id = ?", id).Error
	return &claim, err
}

func (r *ClaimRepository) FindByToken(token string) (*models.OfferClaim, error) {
	var claim models.OfferClaim
	err := r.DB.First(&claim, "token = ?", token).Error
	return &claim, err
}

func (r *ClaimRepository) FindByShortCode(code string) (*models.OfferClaim, error) {
	var claim models.OfferClaim
	err := r.DB.First(&claim, "short_code = ?", code).Error
	return &claim, err
}

// Transition moves a claim to `to` only if its current status is one of
// `from` and it is not expired. Returns true when exactly one row changed.
func (r *ClaimRepository) Transition(claimID uuid.UUID, from []models.ClaimStatus, to models.ClaimStatus, now time.Time) (bool, error) {
	res := r.DB.Model(&models.OfferClaim{}).
		Where("id = ? AND status IN ? AND expires_at > ?", claimID, from, now).
		Update("status", to)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Redeem marks a RECLAMADO claim as CANJEADO with its audit fields, atomically.
func (r *ClaimRepository) Redeem(claimID, redeemerID uuid.UUID, device string, now time.Time) (bool, error) {
	res := r.DB.Model(&models.OfferClaim{}).
		Where("id = ? AND status = ? AND expires_at > ?", claimID, models.ClaimClaimed, now).
		Updates(map[string]interface{}{
			"status":          models.ClaimRedeemed,
			"redeemed_at":     now,
			"redeemed_by":     redeemerID,
			"redeemed_device": device,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// MarkExpired sets a RECLAMADO claim to EXPIRADO when past its window.
func (r *ClaimRepository) MarkExpired(claimID uuid.UUID, now time.Time) (bool, error) {
	res := r.DB.Model(&models.OfferClaim{}).
		Where("id = ? AND status = ? AND expires_at <= ?", claimID, models.ClaimClaimed, now).
		Update("status", models.ClaimExpired)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// CountActiveByOfferAndUser counts the user's unexpired, non-canceled claims
// for an offer (repetition policy: "once" = at most 1).
func (r *ClaimRepository) CountActiveByOfferAndUser(offerID, userID uuid.UUID, now time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.OfferClaim{}).
		Where("offer_id = ? AND user_id = ? AND status IN ? AND expires_at > ?",
			offerID, userID,
			[]models.ClaimStatus{models.ClaimClaimed, models.ClaimRedeemed},
			now).
		Count(&count).Error
	return count, err
}

// CountRedeemedTodayByOfferAndUser supports the "daily" repetition policy.
func (r *ClaimRepository) CountRedeemedByOfferAndUserSince(offerID, userID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.OfferClaim{}).
		Where("offer_id = ? AND user_id = ? AND status = ? AND redeemed_at >= ?",
			offerID, userID, models.ClaimRedeemed, since).
		Count(&count).Error
	return count, err
}

// CountClaimsByUserSince caps how many claims a user can make per day.
func (r *ClaimRepository) CountClaimsByUserSince(userID uuid.UUID, since time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.OfferClaim{}).
		Where("user_id = ? AND claimed_at >= ?", userID, since).
		Count(&count).Error
	return count, err
}

// FindByUser lists a user's claims for the wallet/active view.
func (r *ClaimRepository) FindByUser(userID uuid.UUID) ([]models.OfferClaim, error) {
	var claims []models.OfferClaim
	err := r.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&claims).Error
	return claims, err
}
