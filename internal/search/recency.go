package search

import (
	"math"
	"time"
)

// recencyWeight returns a value from 0.0 (old) to 1.0 (now) using exponential
// decay with a 7-day half-life. Notes updated just now score ~1.0; notes
// updated 7 days ago score ~0.5; 14 days ago ~0.25, etc.
func recencyWeight(updatedAt time.Time) float64 {
	age := time.Since(updatedAt)
	if age < 0 {
		age = 0
	}
	halfLife := 7 * 24 * time.Hour // 7-day half-life
	// ln(2) = 0.693147...
	return math.Exp(-0.693147 * float64(age) / float64(halfLife))
}
