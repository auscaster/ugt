package access

import (
	"slices"
	"strings"
	"testing"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

const ws = "/ugt/workspaces/a/b/c"

// offline is a service that reaches nothing beyond the sandbox.
func offline() compose.Service {
	return compose.Service{NetworkMode: "none", Mounts: []compose.Mount{
		{Type: "bind", Source: ws + "/inputs", Target: "/in", ReadOnly: true},
		{Type: "bind", Source: ws + "/outputs", Target: "/out"},
		{Type: "bind", Source: ws + "/recipe/check", Target: "/check", ReadOnly: true},
	}}
}

func kinds(s compose.Service, m compose.Model) []Kind {
	if m.Services == nil {
		m.Services = map[string]compose.Service{}
	}
	m.Services["s"] = s
	return KindsOf(Grants(m, ws))
}

func TestTheSandboxGrantsNothing(t *testing.T) {
	if got := kinds(offline(), compose.Model{}); len(got) != 0 {
		t.Fatalf("grants %v", got)
	}
}

func TestEachKindIsRead(t *testing.T) {
	with := func(f func(*compose.Service)) compose.Service {
		s := offline()
		f(&s)
		return s
	}
	cases := []struct {
		name string
		s    compose.Service
		m    compose.Model
		want Kind
	}{
		{"default network", with(func(s *compose.Service) { s.NetworkMode = "" }), compose.Model{}, Internet},
		{"bridge", with(func(s *compose.Service) { s.NetworkMode = "bridge" }), compose.Model{}, Internet},
		{"public port", with(func(s *compose.Service) { s.Ports = []compose.Port{{Target: 80, Published: "8080"}} }), compose.Model{}, LAN},
		{"all interfaces", with(func(s *compose.Service) { s.Ports = []compose.Port{{Target: 80, HostIP: "0.0.0.0"}} }), compose.Model{}, LAN},
		{"host-gateway", with(func(s *compose.Service) { s.ExtraHosts = []string{"ollama=host-gateway"} }), compose.Model{}, HostServices},
		{"home folder", with(func(s *compose.Service) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "bind", Source: "/home/kam/Pictures", Target: "/photos", ReadOnly: true})
		}), compose.Model{}, HostFolder},
		{"volume bound to a host path", with(func(s *compose.Service) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "volume", Source: "media", Target: "/media"})
		}), compose.Model{Volumes: map[string]compose.Volume{"media": {DriverOpts: map[string]string{"type": "none", "o": "bind", "device": "/srv/media"}}}}, HostFolder},
		{"engine socket", with(func(s *compose.Service) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "bind", Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"})
		}), compose.Model{}, Engine},
		{"existing volume", with(func(s *compose.Service) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "volume", Source: "theirs", Target: "/data"})
		}), compose.Model{Volumes: map[string]compose.Volume{"theirs": {External: true}}}, Engine},
		{"another container's network", with(func(s *compose.Service) { s.NetworkMode = "container:vpn" }), compose.Model{}, Engine},
		{"build with ssh", with(func(s *compose.Service) { s.Build = &compose.Build{SSH: []string{"default"}} }), compose.Model{}, SSHAgent},
		{"device", with(func(s *compose.Service) { s.Devices = []string{"/dev/ttyUSB0"} }), compose.Model{}, Device},
		{"capabilities", with(func(s *compose.Service) { s.CapAdd = []string{"NET_ADMIN"} }), compose.Model{}, Capabilities},
		{"privileged", with(func(s *compose.Service) { s.Privileged = true }), compose.Model{}, Privileged},
		{"unconfined", with(func(s *compose.Service) { s.SecurityOpt = []string{"seccomp=unconfined"} }), compose.Model{}, Privileged},
		{"build on the host network", with(func(s *compose.Service) { s.Build = &compose.Build{Network: "host"} }), compose.Model{}, Privileged},
		{"host network", with(func(s *compose.Service) { s.NetworkMode = "host" }), compose.Model{}, HostNamespace},
		{"host processes", with(func(s *compose.Service) { s.PID = "host" }), compose.Model{}, HostNamespace},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := kinds(c.s, c.m); !slices.Contains(got, c.want) {
				t.Errorf("grants %v, want %s", got, c.want)
			}
		})
	}
}

// A service on only internal networks, or behind one that has none, does not
// reach the internet.
func TestInternetFollowsTheNetworks(t *testing.T) {
	m := compose.Model{
		Networks: map[string]compose.Network{"default": {}, "inside": {Internal: true}},
		Services: map[string]compose.Service{
			"db":    {Networks: []string{"inside"}},
			"api":   {Networks: []string{"default", "inside"}},
			"side":  {NetworkMode: "service:db"},
			"proxy": {NetworkMode: "service:api"},
		},
	}
	reach := map[string]bool{}
	for _, g := range Grants(m, ws) {
		if g.Kind == Internet {
			reach[g.Service] = true
		}
	}
	if reach["db"] || reach["side"] || !reach["api"] || !reach["proxy"] {
		t.Errorf("internet %v", reach)
	}
}

func TestCheck(t *testing.T) {
	s := offline()
	s.Privileged = true
	m := compose.Model{Services: map[string]compose.Service{"task": s}}

	problems := Check(nil, m, ws)
	if len(problems) != 1 || !strings.Contains(problems[0], `{"kind": "privileged", "why": "..."}`) || !strings.HasPrefix(problems[0], "services.task runs privileged") {
		t.Errorf("undeclared: %v", problems)
	}
	if problems := Check([]recipe.Access{{Kind: "privileged", Why: "it flashes firmware"}}, m, ws); len(problems) != 0 {
		t.Errorf("declared: %v", problems)
	}
	problems = Check([]recipe.Access{{Kind: "privileged", Why: "x"}, {Kind: "privileged", Why: "y"}, {Kind: "lan", Why: "z"}}, m, ws)
	if len(problems) != 2 || !strings.Contains(problems[0], "declares privileged twice") || !strings.Contains(problems[1], "declares lan, but nothing") {
		t.Errorf("duplicate and unused: %v", problems)
	}
}

// A workspace folder other than the sandbox's own is refused, whatever is
// declared: data belongs in a named volume.
func TestCheckRefusesOtherWorkspaceFolders(t *testing.T) {
	s := offline()
	s.Mounts = append(s.Mounts, compose.Mount{Type: "bind", Source: ws + "/data", Target: "/data"})
	problems := Check([]recipe.Access{{Kind: "host-folder", Why: "x"}}, compose.Model{Services: map[string]compose.Service{"task": s}}, ws)
	if len(problems) != 2 || !strings.Contains(problems[1], "keep data in a named volume") {
		t.Errorf("problems %v", problems)
	}
}

// On Windows with an existing engine, Compose prints Windows paths.
func TestWindowsPaths(t *testing.T) {
	const win = `C:\Users\kam\AppData\Local\UGT\workspaces\a\b\c`
	s := compose.Service{NetworkMode: "none", Mounts: []compose.Mount{{Type: "bind", Source: win + `\inputs`, Target: "/in"}}}
	if got := KindsOf(Grants(compose.Model{Services: map[string]compose.Service{"s": s}}, win)); len(got) != 0 {
		t.Errorf("grants %v", got)
	}
}

func TestOnlyInternetSkipsTheMaintainer(t *testing.T) {
	for _, k := range Kinds {
		if k.Gated() == (k == Internet) {
			t.Errorf("%s gated %v", k, k.Gated())
		}
	}
}
