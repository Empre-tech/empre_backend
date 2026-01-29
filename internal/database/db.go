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
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort)

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
