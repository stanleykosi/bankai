/**
 * @description
 * Market and Event database models.
 * Maps to the 'markets' table in PostgreSQL.
 *
 * @dependencies
 * - gorm.io/gorm
 * - github.com/lib/pq (for string array support if needed, though we use string for simplicity or specialized types)
 */

package models

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// StringArray is a helper type to handle string arrays in Postgres (TEXT[])
type StringArray []string

// Scan implements the sql.Scanner interface
func (a *StringArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	switch v := src.(type) {
	case []byte:
		// PostgreSQL returns arrays as strings like "{value1,value2,value3}"
		return a.parsePostgresArray(string(v))
	case string:
		return a.parsePostgresArray(v)
	default:
		return errors.New("type assertion failed for StringArray")
	}
}

// parsePostgresArray parses PostgreSQL array format: {value1,value2,value3}
func (a *StringArray) parsePostgresArray(s string) error {
	if s == "{}" || s == "" {
		*a = []string{}
		return nil
	}
	
	// Remove curly braces
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	
	if s == "" {
		*a = []string{}
		return nil
	}
	
	// Split by comma, handling quoted values
	// Simple approach: split by comma (works for most cases)
	// For production, might need more sophisticated parsing for quoted values with commas
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Remove quotes if present
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			part = part[1 : len(part)-1]
		}
		result = append(result, part)
	}
	*a = result
	return nil
}

// Value implements the driver.Valuer interface
// Returns PostgreSQL array format: {value1,value2,value3}
func (a StringArray) Value() (driver.Value, error) {
	if a == nil || len(a) == 0 {
		return "{}", nil
	}
	
	// Format as PostgreSQL array: {value1,value2,value3}
	// Escape and quote values that contain special characters
	quoted := make([]string, len(a))
	for i, v := range a {
		// Quote values that contain commas, quotes, backslashes, or spaces
		if strings.ContainsAny(v, `,"\{} `) {
			// Escape backslashes and quotes
			escaped := strings.ReplaceAll(v, `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, `"`, `\"`)
			quoted[i] = fmt.Sprintf(`"%s"`, escaped)
		} else {
			quoted[i] = v
		}
	}
	return fmt.Sprintf("{%s}", strings.Join(quoted, ",")), nil
}

// Market represents a Polymarket market (contract)
// Maps to the 'markets' table
type Market struct {
	ConditionID     string      `gorm:"primaryKey;column:condition_id" json:"condition_id"`
	QuestionID      string      `gorm:"column:question_id" json:"question_id"`
	Slug            string      `gorm:"column:slug;index" json:"slug"`
	Title           string      `gorm:"column:title" json:"title"`
	Description     string      `gorm:"column:description" json:"description"`
	ResolutionRules string      `gorm:"column:resolution_rules" json:"resolution_rules"`
	Category        string      `gorm:"column:category" json:"category"`
	Tags            StringArray  `gorm:"column:tags;type:text[]" json:"tags"` // Requires handling for postgres array if using raw SQL, or JSON for simplicity
	Active          bool        `gorm:"column:active;default:true" json:"active"`
	Closed          bool        `gorm:"column:closed;default:false" json:"closed"`
	Archived        bool        `gorm:"column:archived;default:false" json:"archived"`
	TokenIDYes      string      `gorm:"column:token_id_yes" json:"token_id_yes"`
	TokenIDNo       string      `gorm:"column:token_id_no" json:"token_id_no"`
	Volume24h       float64     `gorm:"column:volume_24h" json:"volume_24h"`
	Liquidity       float64     `gorm:"column:liquidity" json:"liquidity"`
	EndDate         *time.Time  `gorm:"column:end_date" json:"end_date"`
	CreatedAt       time.Time   `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName overrides the table name used by Market to `markets`
func (Market) TableName() string {
	return "markets"
}

