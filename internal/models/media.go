package models

import (
	"time"

	"github.com/google/uuid"
)

type Media struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	S3Key        string    `gorm:"not null" json:"-"` // Hidden from JSON
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	URL          string    `gorm:"-" json:"url"` // Virtual field
}
