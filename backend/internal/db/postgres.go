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
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

// ConnectPostgres initializes the PostgreSQL connection
func ConnectPostgres(cfg *config.Config) (*gorm.DB, error) {
	// Configure GORM logger based on environment
	gormLogLevel := gormLogger.Error
	if cfg.Server.Env == "development" {
		gormLogLevel = gormLogger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.DB.URL), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogLevel),
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

	logger.Info("✅ Connected to PostgreSQL")
	return db, nil
}

