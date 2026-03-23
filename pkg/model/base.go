// Package model provides the model registry, metadata extraction, and generic
// CRUD operations for the GoFrame framework. It uses reflection to extract
// struct metadata at registration time and GORM for database operations.
package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel provides the standard fields that all GoFrame models should embed.
// It is the equivalent of Django's models.Model.
type BaseModel struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
