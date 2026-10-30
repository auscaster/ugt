package placement

import (
	"strings"
	"testing"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		n    Needs
		h    Host
		run  bool
		want string
	}{
		{"fits", Needs{MemoryBytes: 3 << 30}, Host{Runtime: "host", MemoryBytes: 8 << 30}, true, ""},
		{"unknown memory runs", Needs{MemoryBytes: 3 << 30}, Host{}, true, ""},
		{"too big", Needs{MemoryBytes: 5 << 30}, Host{Runtime: "wsl", MemoryBytes: 4 << 30}, false, ".wslconfig"},
		{"gpu", Needs{GPU: true}, Host{MemoryBytes: 8 << 30}, false, "GPU"},
	}
	for _, c := range cases {
		d := Decide(c.n, c.h)
		if d.Run != c.run || !strings.Contains(d.Reason+" "+d.Fix, c.want) {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
}
