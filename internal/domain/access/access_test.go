package access

import (
	"slices"
	"strings"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/compose"
	"github.com/usegeekthing/ugt/internal/domain/recipe"
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
	// the project's own volume and network, by the names Compose gives them
	s := offline()
	s.NetworkMode, s.Networks = "", []string{"inside"}
	s.Mounts = append(s.Mounts, compose.Mount{Type: "volume", Source: "data", Target: "/data"})
	m := compose.Model{Project: "probe", Volumes: map[string]compose.Volume{"data": {Name: "probe_data"}},
		Networks: map[string]compose.Network{"inside": {Name: "probe_inside", Internal: true}}}
	if got := kinds(s, m); len(got) != 0 {
		t.Fatalf("own volume and network grant %v", got)
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
		{"the engine's API socket", with(func(s *compose.Service) { s.APISocket = true }), compose.Model{}, Engine},
		{"a privileged hook", with(func(s *compose.Service) { s.HookPrivileged = true }), compose.Model{}, Privileged},
		{"another project's volume, by name", with(func(s *compose.Service) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "volume", Source: "data", Target: "/data"})
		}), compose.Model{Project: "probe", Volumes: map[string]compose.Volume{"data": {Name: "other-recipe_data"}}}, Engine},
		{"another project's network, by name", with(func(s *compose.Service) { s.NetworkMode, s.Networks = "", []string{"shared"} }),
			compose.Model{Project: "probe", Networks: map[string]compose.Network{"shared": {Name: "other-recipe_default"}}}, Engine},
		{"a build context outside the workspace", with(func(s *compose.Service) {
			s.Build = &compose.Build{Contexts: map[string]string{"host": "/", "base": "docker-image://alpine"}}
		}), compose.Model{}, HostFolder},
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

// What Check refuses whatever is declared: a key UGT cannot say the access
// of, the recipe's own files mounted writable, a build reading another
// workspace folder, and output sent anywhere but the engine's own logs.
func TestCheckRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		change func(*compose.Service, *compose.Model)
		want   string
	}{
		"an unknown key": {func(_ *compose.Service, m *compose.Model) { m.Unknown = []string{"services.task.runtime"} },
			"compose.yaml sets services.task.runtime, which UGT cannot yet say the access of: remove it"},
		"a writable recipe folder": {func(s *compose.Service, _ *compose.Model) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "bind", Source: ws + "/recipe", Target: "/recipe"})
		}, "services.task mounts " + ws + "/recipe writable"},
		"a build of another workspace folder": {func(s *compose.Service, _ *compose.Model) {
			s.Build = &compose.Build{Contexts: map[string]string{"data": ws + "/inputs"}}
		}, "services.task builds with " + ws + "/inputs, a folder of the workspace"},
		"a log driver": {func(s *compose.Service, _ *compose.Model) { s.LogDriver = "syslog" },
			"services.task sends its output through the log driver syslog"},
		// what runs can put a link in inputs, which a mount of a folder within would follow
		"a folder within inputs": {func(s *compose.Service, _ *compose.Model) {
			s.Mounts = append(s.Mounts, compose.Mount{Type: "bind", Source: ws + "/inputs/edit", Target: "/edit"})
		}, "services.task mounts " + ws + "/inputs/edit, a folder of the workspace"},
	} {
		t.Run(name, func(t *testing.T) {
			s, m := offline(), compose.Model{}
			c.change(&s, &m)
			m.Services = map[string]compose.Service{"task": s}
			problems := Check(nil, m, ws)
			if len(problems) != 1 || !strings.HasPrefix(problems[0], c.want) {
				t.Errorf("problems %q, want %q", problems, c.want)
			}
		})
	}
	s := offline()
	s.Build = &compose.Build{Contexts: map[string]string{"lib": ws + "/recipe/lib"}}
	s.LogDriver = "json-file"
	if problems := Check(nil, compose.Model{Services: map[string]compose.Service{"task": s}}, ws); len(problems) != 0 {
		t.Errorf("the sandbox's own: %q", problems)
	}
}
