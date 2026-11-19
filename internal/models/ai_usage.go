package models

import (
	"time"

	"github.com/google/uuid"
)

// AIUsage es una llamada a la IA que salió bien, con los tokens que la IA dijo haber usado (2026-11-19). Sirve para
// saber cuánto cuesta de verdad cada función y cada negocio, y ajustar topes y precios con datos.
type AIUsage struct {
	ID               uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	Feature          string     `gorm:"size:32;not null;index:idx_ai_usage_feature,priority:1" json:"feature"`
	EntityID         *uuid.UUID `gorm:"type:uuid;index:idx_ai_usage_entity,priority:1" json:"entity_id,omitempty"`
	UserID           *uuid.UUID `gorm:"type:uuid" json:"user_id,omitempty"`
	Model            string     `gorm:"size:64" json:"model"`
	InputTokens      int        `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens     int        `gorm:"not null;default:0" json:"output_tokens"`
	CacheReadTokens  int        `gorm:"not null;default:0" json:"cache_read_tokens"`
	CacheWriteTokens int        `gorm:"not null;default:0" json:"cache_write_tokens"`
	LatencyMS        int64      `gorm:"not null;default:0" json:"latency_ms"`
	CreatedAt        time.Time  `gorm:"not null;index;index:idx_ai_usage_feature,priority:2;index:idx_ai_usage_entity,priority:2" json:"created_at"`
}

func (AIUsage) TableName() string { return "ai_usage" }
