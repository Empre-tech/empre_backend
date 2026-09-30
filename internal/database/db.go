package database

import (
	"fmt"
	"log"
	"time"

	"empre_backend/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func ConnectDB(cfg *config.Config) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort, cfg.DBSSLMode)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		PrepareStmt: true, // Cache prepared statements for better performance
	})
	if err != nil {
		log.Fatal("Failed to connect to database: ", err)
	}

	// Optimize Connection Pool
	sqlDB, err := DB.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	// Enable PostGIS extension if not exists
	// DB.Exec("CREATE EXTENSION IF NOT EXISTS postgis;")

	log.Println("Database connection established")
}

// PrepareForPostMigration handles a one-time schema transition: the Post
// model was introduced after entity_photos already existed with rows that
// have no post_id (every photo used to be its own standalone post). GORM's
// AutoMigrate can't add the new post_id column as NOT NULL while those old
// rows exist ("column post_id ... contains null values").
//
// Since this is a pre-launch app with only dev/test data, the pragmatic
// fix is: if entity_photos exists but doesn't have a post_id column yet,
// wipe it (cascading to anything that references it) so AutoMigrate can
// create the column cleanly. This only ever does anything once — after the
// column exists, it's a no-op on every future start.
func PrepareForPostMigration(db *gorm.DB) error {
	var tableExists bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'entity_photos'
	)`).Scan(&tableExists).Error; err != nil {
		return err
	}
	if !tableExists {
		return nil
	}

	var hasPostID bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'entity_photos' AND column_name = 'post_id'
	)`).Scan(&hasPostID).Error; err != nil {
		return err
	}
	if hasPostID {
		return nil
	}

	log.Println("Migrando entity_photos al nuevo modelo de Post: la tabla es de antes de Post y no tiene post_id. Como son solo datos de desarrollo, se vacía entity_photos para que la migración pueda crear la columna.")
	if err := db.Exec("TRUNCATE TABLE entity_photos CASCADE").Error; err != nil {
		return err
	}
	return nil
}
