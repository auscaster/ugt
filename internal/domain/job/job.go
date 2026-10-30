// Package job models a long operation (setup, verify, run) that an agent
// starts with one call and follows with another, because builds outlast an
// agent's tool time limit.
package job

import (
	"crypto/rand"
	"encoding/binary"
	"math/big"
	"time"
)

// State of a job.
type State string

const (
	Queued    State = "queued"
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

// Done reports whether the job has finished, one way or another.
func (s State) Done() bool { return s == Succeeded || s == Failed || s == Cancelled }

// Job is one long operation and what it has said so far.
type Job struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // "verify", "run", "setup"
	Recipe  string    `json:"recipe,omitempty"`
	State   State     `json:"state"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
	Events  []Event   `json:"events"`
	Error   string    `json:"error,omitempty"`
	Result  any       `json:"result,omitempty"`
}

// Event is one line of progress.
type Event struct {
	Time    time.Time `json:"time"`
	Step    string    `json:"step,omitempty"`
	Message string    `json:"message"`
}

// crockford is the ULID alphabet.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns "job_" and a ULID: 48 bits of milliseconds then 80 random
// bits, so IDs sort by start time.
func NewID(now time.Time) string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], uint64(now.UnixMilli())<<16)
	if _, err := rand.Read(b[6:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	// 128 bits as 26 base-32 digits, most significant first.
	n := new(big.Int).SetBytes(b[:])
	digits := make([]byte, 26)
	mask := big.NewInt(31)
	for i := 25; i >= 0; i-- {
		digits[i] = crockford[new(big.Int).And(n, mask).Int64()]
		n.Rsh(n, 5)
	}
	return "job_" + string(digits)
}
