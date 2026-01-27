package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type VerificationStatus string

const (
	StatusPending  VerificationStatus = "pending"
	StatusVerified VerificationStatus = "verified"
	StatusRejected VerificationStatus = "rejected"
)

type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Entity struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	OwnerID     uuid.UUID `gorm:"type:uuid;not null" json:"owner_id"`
	Name        string    `gorm:"not null" json:"name"`
	Description string    `json:"description"`
	CategoryID  uuid.UUID `gorm:"type:uuid;not null" json:"category_id"` // e.g., "Food", "Services"
	Address     string    `json:"address"`
	City        string    `json:"city"`
	ContactInfo string    `json:"contact_info"`
	BannerURL   string    `json:"banner_url"`
	ProfileURL  string    `json:"profile_url"`
	// PostGIS Geography Point (SRID 4326)
	// PostGIS Geography Point (SRID 4326)
	// Location           string             `gorm:"type:geography(POINT,4326)" json:"-"`
	Latitude           float64            `gorm:"type:float" json:"latitude"`
	Longitude          float64            `gorm:"type:float" json:"longitude"`
	VerificationStatus VerificationStatus `gorm:"type:varchar(20);default:'pending'" json:"verification_status"`
	IsVerified         bool               `gorm:"default:false" json:"is_verified"` // Check dorado
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	DeletedAt          gorm.DeletedAt     `gorm:"index" json:"-"`

	// Associations
	Owner    User          `gorm:"foreignKey:OwnerID" json:"-"`
	Category Category      `gorm:"foreignKey:CategoryID" json:"category"`
	Photos   []EntityPhoto `gorm:"foreignKey:EntityID" json:"photos"`
}

// BeforeSave hook to update Location field from Lat/Long
func (e *Entity) BeforeSave(tx *gorm.DB) (err error) {
	// Format: POINT(longitude latitude)
	// Example: POINT(-75.5 10.4)
	// if e.Latitude != 0 && e.Longitude != 0 {
	// 	e.Location = fmt.Sprintf("POINT(%f %f)", e.Longitude, e.Latitude)
	// }
	return
}
