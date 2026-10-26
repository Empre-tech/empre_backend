package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"empre_backend/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func ConnectDB(cfg *config.Config) {
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort, cfg.DBSSLMode)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		PrepareStmt: true, // Cache prepared statements for better performance
		// SQL lento (3.53): umbral configurable con DB_SLOW_MS (200 ms por omisión), sin colores ANSI en los logs y
		// sin avisar de «no encontrado» (es un resultado normal, no un problema).
		Logger: logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
			SlowThreshold:             slowThreshold(),
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	if err != nil {
		log.Fatal("Failed to connect to database: ", err)
	}

	// Optimize Connection Pool
	sqlDB, err := DB.DB()
	if err == nil {
		// Por defecto 15 abiertas (lo que admite el pooler); con 100 el pooler rechazaba y daba 500.
		open, idle := config.NormalizePool(cfg.DBMaxOpenConns, cfg.DBMaxIdleConns)
		sqlDB.SetMaxIdleConns(idle)
		sqlDB.SetMaxOpenConns(open)
		sqlDB.SetConnMaxLifetime(time.Hour)
		sqlDB.SetConnMaxIdleTime(config.IdleTime(cfg.DBMaxIdleMinutes))
	}

	// Enable PostGIS extension if not exists
	// DB.Exec("CREATE EXTENSION IF NOT EXISTS postgis;")

	log.Println("Database connection established")
}

// WarmPool abre n conexiones a la vez y las deja inactivas en el pool (3.54): así la primera pantalla después de
// arrancar no paga abrirlas (TLS y autenticación con la base remota). Toma las n al mismo tiempo para que sean n
// distintas. Devuelve cuántas quedaron abiertas.
func WarmPool(ctx context.Context, sqlDB *sql.DB, n int) int {
	if n <= 0 {
		return 0
	}
	conns := make([]*sql.Conn, n)
	var wg sync.WaitGroup
	for i := range conns {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := sqlDB.Conn(ctx)
			if err != nil {
				return
			}
			if err := c.PingContext(ctx); err != nil {
				_ = c.Close()
				return
			}
			conns[i] = c
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range conns {
		if c != nil {
			_ = c.Close() // vuelve al pool como inactiva (no cierra la conexión real)
			ok++
		}
	}
	return ok
}

func slowThreshold() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("DB_SLOW_MS")); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 200 * time.Millisecond
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
