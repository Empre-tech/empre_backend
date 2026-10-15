package database

import (
	"log"

	"empre_backend/internal/models"
	"empre_backend/internal/repository"
	"empre_backend/pkg/utils"

	"gorm.io/gorm"
)

// EnsureClaimIndexes creates the partial unique index that allows at most one
// open (RECLAMADO) claim per user and offer, so two simultaneous claims from
// the same user can't both reserve a slot. Idempotent.
//
// Before the index existed, the "daily" policy let a user hold several open
// claims on the same offer, so existing data may violate it. Those duplicates
// are cleaned first: overdue claims are expired, and of the ones still open
// only the most recent is kept (the rest are canceled). Slots are released in
// both cases.
func EnsureClaimIndexes(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		now := utils.NowInCartagena()

		expired, err := repository.NewClaimRepository(tx).ExpireOverdue(now)
		if err != nil {
			return err
		}

		var canceled int64
		if err := tx.Raw(`
			WITH ranked AS (
				SELECT id, ROW_NUMBER() OVER (
					PARTITION BY offer_id, user_id ORDER BY claimed_at DESC, id
				) AS rn
				FROM offer_claims
				WHERE status = ? AND deleted_at IS NULL
			), canceled AS (
				UPDATE offer_claims c
				SET status = ?, updated_at = ?
				FROM ranked
				WHERE c.id = ranked.id AND ranked.rn > 1
				RETURNING c.offer_id
			), per_offer AS (
				SELECT offer_id, COUNT(*) AS n FROM canceled GROUP BY offer_id
			), released AS (
				UPDATE offers o
				SET used_slots = GREATEST(o.used_slots - per_offer.n, 0)
				FROM per_offer
				WHERE o.id = per_offer.offer_id
				RETURNING o.id
			)
			SELECT COALESCE(SUM(n), 0) FROM per_offer`,
			models.ClaimClaimed, models.ClaimCanceled, now,
		).Scan(&canceled).Error; err != nil {
			return err
		}

		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + models.OpenClaimIndex + `
			ON offer_claims (offer_id, user_id)
			WHERE status = '` + string(models.ClaimClaimed) + `' AND deleted_at IS NULL`).Error; err != nil {
			return err
		}

		if expired > 0 || canceled > 0 {
			log.Printf("Reclamos: %d vencidos expirados y %d duplicados cancelados antes de crear %s", expired, canceled, models.OpenClaimIndex)
		}
		return nil
	})
}
