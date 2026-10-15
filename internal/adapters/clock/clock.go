// Package clock implements ports.Clock with the system clock.
package clock

import "time"

// System is the real clock.
type System struct{}

// Now implements ports.Clock.
func (System) Now() time.Time { return time.Now().UTC() }
