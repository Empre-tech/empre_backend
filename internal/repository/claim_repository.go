package repository

import (
	"strings"
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

// CountActiveByOfferAndUser counts the user's claims that block a new one
// under the "once" policy: any redeemed claim (forever, regardless of its
// window) plus any still-open RECLAMADO claim.
func (r *ClaimRepository) CountActiveByOfferAndUser(offerID, userID uuid.UUID, now time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.OfferClaim{}).
		Where("offer_id = ? AND user_id = ? AND (status = ? OR (status = ? AND expires_at > ?))",
			offerID, userID, models.ClaimRedeemed, models.ClaimClaimed, now).
		Count(&count).Error
	return count, err
}

// CountOpenByOfferAndUser counts the user's still-open RECLAMADO claims for an
// offer. Under the "daily" policy a user may hold at most one at a time.
func (r *ClaimRepository) CountOpenByOfferAndUser(offerID, userID uuid.UUID, now time.Time) (int64, error) {
	var count int64
	err := r.DB.Model(&models.OfferClaim{}).
		Where("offer_id = ? AND user_id = ? AND status = ? AND expires_at > ?",
			offerID, userID, models.ClaimClaimed, now).
		Count(&count).Error
	return count, err
}

// ExpireOverdue moves every RECLAMADO claim past its window to EXPIRADO and
// releases its slot. Run periodically so slots held by no-shows come back.
func (r *ClaimRepository) ExpireOverdue(now time.Time) (int64, error) {
	return r.expireOverdue(now, "")
}

// ExpireOverdueFor does the same for one user's claims on one offer. Claim
// calls it first so a lapsed claim the sweeper has not reached yet neither
// blocks a new one nor keeps its slot.
func (r *ClaimRepository) ExpireOverdueFor(offerID, userID uuid.UUID, now time.Time) (int64, error) {
	return r.expireOverdue(now, "AND offer_id = ? AND user_id = ?", offerID, userID)
}

// expireOverdue expires and releases in a single statement, so a claim is
// never expired without its slot coming back, nor released twice.
func (r *ClaimRepository) expireOverdue(now time.Time, filter string, filterArgs ...interface{}) (int64, error) {
	args := append([]interface{}{models.ClaimExpired, now, models.ClaimClaimed, now}, filterArgs...)
	var expired int64
	err := r.DB.Raw(`
		WITH expired AS (
			UPDATE offer_claims
			SET status = ?, updated_at = ?
			WHERE status = ? AND expires_at <= ? AND deleted_at IS NULL `+filter+`
			RETURNING offer_id
		), per_offer AS (
			SELECT offer_id, COUNT(*) AS n FROM expired GROUP BY offer_id
		), released AS (
			UPDATE offers o
			SET used_slots = GREATEST(o.used_slots - per_offer.n, 0)
			FROM per_offer
			WHERE o.id = per_offer.offer_id
			RETURNING o.id
		)
		SELECT COALESCE(SUM(n), 0) FROM per_offer`,
		args...,
	).Scan(&expired).Error
	return expired, err
}

// IsDuplicateOpenClaim reports whether err is the violation of the
// one-open-claim-per-user-and-offer index (see database.EnsureClaimIndexes).
func IsDuplicateOpenClaim(err error) bool {
	return err != nil && strings.Contains(err.Error(), models.OpenClaimIndex)
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

// FindRecentByUser devuelve los reclamos de un usuario hechos desde `since`,
// los más nuevos primero. Es lo que alimenta «mis reclamos»: los abiertos (cuya
// ventana dura como mucho 24 h) y los que acaban de terminar.
func (r *ClaimRepository) FindRecentByUser(userID uuid.UUID, since time.Time, limit int) ([]models.OfferClaim, error) {
	var claims []models.OfferClaim
	err := r.DB.Where("user_id = ? AND claimed_at >= ?", userID, since).
		Order("claimed_at DESC").Limit(limit).Find(&claims).Error
	return claims, err
}

// EntityNames devuelve el nombre de cada negocio pedido, en una sola consulta.
func (r *ClaimRepository) EntityNames(ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   uuid.UUID
		Name string
	}
	if err := r.DB.Model(&models.Entity{}).Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.Name
	}
	return out, nil
}

// FindByUser lists a user's claims for the wallet/active view.
func (r *ClaimRepository) FindByUser(userID uuid.UUID) ([]models.OfferClaim, error) {
	var claims []models.OfferClaim
	err := r.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&claims).Error
	return claims, err
}
