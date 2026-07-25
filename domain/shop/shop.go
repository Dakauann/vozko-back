package shop

import (
	"errors"
	"time"
)

const MaxShopsPerUser = 1

var (
	ErrShopNotFound       = errors.New("shop not found")
	ErrShopNameRequired   = errors.New("shop name is required")
	ErrShopBrandRequired  = errors.New("shop brand is required")
	ErrShopLogoRequired   = errors.New("shop logo is required")
	ErrShopNameTooShort   = errors.New("shop name must be at least 3 characters")
	ErrShopNameTooLong    = errors.New("shop name must not exceed 255 characters")
	ErrUserAlreadyHasShop = errors.New("user already has a shop")
	ErrShopLimitReached   = errors.New("user has reached the maximum limit of 5 shops")
	ErrInvalidMediaID     = errors.New("invalid media ID")
	ErrUnauthorized       = errors.New("unauthorized")
)

type ShopStatus string

const (
	ShopStatusPending   ShopStatus = "pending"
	ShopStatusActive    ShopStatus = "active"
	ShopStatusSuspended ShopStatus = "suspended"
	ShopStatusClosed    ShopStatus = "closed"
)

type Shop struct {
	ID            int64      `json:"id"`
	UserID        string     `json:"user_id"`
	Name          string     `json:"name"`
	Brand         string     `json:"brand"`
	LogoMediaID   string     `json:"logo_media_id"`
	BannerMediaID string     `json:"banner_media_id"`
	ReviewIDs     []string   `json:"review_ids"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Status        ShopStatus `json:"status"`
	Description   string     `json:"description"`
	IsOfficial    bool       `json:"is_official"`
}
