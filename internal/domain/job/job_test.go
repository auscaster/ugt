package job

import (
	"strings"
	"testing"
	"time"
)

func TestNewID(t *testing.T) {
	early := NewID(time.UnixMilli(1_700_000_000_000))
	late := NewID(time.UnixMilli(1_700_000_000_001))
	if !strings.HasPrefix(early, "job_") || len(early) != 4+26 {
		t.Fatalf("ID %q is not job_ and 26 characters", early)
	}
	if strings.Trim(early[4:], crockford) != "" {
		t.Fatalf("ID %q uses characters outside the ULID alphabet", early)
	}
	if !(early < late) {
		t.Fatalf("IDs do not sort by time: %s >= %s", early, late)
	}
	if NewID(time.UnixMilli(5)) == NewID(time.UnixMilli(5)) {
		t.Fatal("two IDs in the same millisecond collided")
	}
}

func TestDone(t *testing.T) {
	for s, want := range map[State]bool{Queued: false, Running: false, Succeeded: true, Failed: true, Cancelled: true} {
		if s.Done() != want {
			t.Errorf("%s.Done() = %v", s, !want)
		}
	}
}
