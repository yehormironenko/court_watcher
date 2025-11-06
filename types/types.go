package types

import (
	"fmt"
	"time"
)

// Court represents a tennis court from kluby.org
type Court struct {
	ID       string
	Name     string
	District string
}

// TimeSlot represents an available booking slot
type TimeSlot struct {
	CourtID   string
	CourtName string
	DateTime  time.Time
	Duration  int // minutes
}

// Slot represents a bookable time slot from search API
type Slot struct {
	ClubID    string  // Court ID from kluby.org (e.g., "park-tennis-academy")
	ClubName  string  // Display name (e.g., "Park Tennis Academy")
	CourtType string  // Court type (e.g., "Hala (hard)", "Odkryte")
	TypeID    string  // typ_obiektu from API
	Date      string  // YYYY-MM-DD
	Time      string  // HH:MM
	Duration  int     // Number of 30-minute intervals (4 = 2 hours)
	Price     string  // Price string (e.g., "60,00")
	URL       string  // Booking URL
}

// UniqueID generates a unique identifier for this slot
func (s *Slot) UniqueID() string {
	return fmt.Sprintf("%s_%s_%s_%s_%s", s.ClubID, s.TypeID, s.CourtType, s.Date, s.Time)
}

// Subscription represents user's notification preferences
type Subscription struct {
	ChatID    int64
	Districts []string
	Courts    []string // Court IDs from kluby.org
	Days      []string // ["Mon", "Tue", "Wed", ...]
	TimeFrom  string   // "18:00"
	TimeTo    string   // "21:00"
}
