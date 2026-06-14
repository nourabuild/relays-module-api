// Package models defines data models for the relays service.
package models

import "time"

// User represents a user in the system
type User struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Account       string    `json:"account"`
	Email         string    `json:"email"`
	Bio           *string   `json:"bio,omitempty"`
	DOB           *string   `json:"dob,omitempty"`
	City          *string   `json:"city,omitempty"`
	Phone         *string   `json:"phone,omitempty"`
	AvatarPhotoID *int      `json:"avatar_photo_id,omitempty"`
	IsAdmin       bool      `json:"is_admin"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NewUser struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account string `json:"account"`
	Email   string `json:"email"`
}

// PublicUser is the profile shape exposed to users other than the account
// owner: no email, phone, DOB, city, or admin flag.
type PublicUser struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Account       string  `json:"account"`
	Bio           *string `json:"bio,omitempty"`
	AvatarPhotoID *int    `json:"avatar_photo_id,omitempty"`
}

// PublicProfile reduces a full User to its publicly visible fields.
func PublicProfile(u User) PublicUser {
	return PublicUser{
		ID:            u.ID,
		Name:          u.Name,
		Account:       u.Account,
		Bio:           u.Bio,
		AvatarPhotoID: u.AvatarPhotoID,
	}
}
