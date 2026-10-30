package ledger

import (
	"testing"
	"time"
)

func TestLive(t *testing.T) {
	t0 := time.Unix(0, 0)
	entries := []Entry{
		{Kind: Image, Name: "ugt/a/b/c:1", Created: t0},
		{Kind: Project, Name: "ugt-a-b-c", Created: t0},
		{Kind: Image, Name: "ugt/a/b/c:1", Created: t0, Removed: t0.Add(time.Hour)},
		{Kind: Volume, Name: "ugt-a-b-c_data", Created: t0},
	}
	live := Live(entries)
	if len(live) != 2 || live[0].Name != "ugt-a-b-c" || live[1].Kind != Volume {
		t.Fatalf("Live = %+v", live)
	}
}
