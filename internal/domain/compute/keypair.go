package compute

import "time"

type KeyPair struct {
	ID          int
	Name        string
	UserID      string
	PublicKey   string
	Fingerprint string
	CreatedAt   time.Time
}
