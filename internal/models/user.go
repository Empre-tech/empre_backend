package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

type User struct {
	ID                uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name              string         `gorm:"not null" json:"name"`
	Email             string         `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash      string         `gorm:"not null" json:"-"`
	Phone             string         `json:"phone"`
	ProfilePictureURL string         `json:"profile_picture_url"`
	Role              Role           `gorm:"type:varchar(20);default:'user'" json:"role"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}
