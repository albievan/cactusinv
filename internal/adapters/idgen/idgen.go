// Package idgen implements ports.IDGenerator with time-ordered UUIDv7s.
package idgen

import "github.com/google/uuid"

// UUIDv7 generates UUIDv7 identifiers.
type UUIDv7 struct{}

// NewID implements ports.IDGenerator. It falls back to a random UUIDv4 if the v7
// generator ever fails, so callers never receive an empty ID.
func (UUIDv7) NewID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
