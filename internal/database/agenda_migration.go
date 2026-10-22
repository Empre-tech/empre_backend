package database

import (
	"log"

	"gorm.io/gorm"
)

// EnsureAgendaConstraints creates what AutoMigrate cannot: the exclusion
// constraint that makes it impossible for two active rows (requests,
// confirmed appointments or manual blocks) of the same business to overlap.
// It is the final guarantee against double booking; the code only gives a
// friendly error on top of it. Idempotent: safe to run on every start.
func EnsureAgendaConstraints(db *gorm.DB) error {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS btree_gist`).Error; err != nil {
		return err
	}
	err := db.Exec(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'appointments_no_overlap') THEN
				ALTER TABLE appointments
					ADD CONSTRAINT appointments_no_overlap
					EXCLUDE USING gist (entity_id WITH =, tstzrange(starts_at, blocks_until) WITH &&)
					WHERE (status IN ('requested', 'confirmed', 'blocked'));
			END IF;
		END $$;
	`).Error
	if err != nil {
		return err
	}
	// Un reintento con la misma clave nunca crea una segunda cita.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS appointments_idem ON appointments (customer_id, idempotency_key) WHERE idempotency_key IS NOT NULL`).Error; err != nil {
		return err
	}
	log.Println("Agenda: restricción anti-solapamiento lista")
	return nil
}
