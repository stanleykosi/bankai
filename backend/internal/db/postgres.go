/**
 * @description
 * PostgreSQL connection manager using GORM.
 * Handles connection pooling and initialization.
 *
 * @dependencies
 * - gorm.io/gorm: ORM library
 * - gorm.io/driver/postgres: Postgres driver
 */

package db

import (
	"log"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ConnectPostgres initializes the PostgreSQL connection
func ConnectPostgres(cfg *config.Config) (*gorm.DB, error) {
	// Configure GORM logger based on environment
	gormLogLevel := logger.Error
	if cfg.Server.Env == "development" {
		gormLogLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.DB.URL), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return nil, err
	}

	// Get generic database object to set connection pool params
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// Set connection pool settings
	// These values should be tuned based on infrastructure limits (e.g. AWS RDS instance size)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Println("✅ Connected to PostgreSQL")
	return db, nil
}

