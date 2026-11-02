// Package access is how a recipe reaches beyond UGT's sandbox: run anything,
// hide nothing. A recipe declares each kind of access it needs in
// recipe.json, with why; the grants it really gets are read from the resolved
// Compose model, so defaults and variables cannot hide one. Nothing is refused
// for needing access, only for not declaring it.
package access

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/recipe"
	"github.com/auscaster/ugt/internal/domain/workspace"
)

// Kind is one way a recipe reaches beyond the sandbox.
type Kind string

// The kinds, in the order they are shown.
const (
	Internet      Kind = "internet"       // a running service reaches the internet
	LAN           Kind = "lan"            // a port is published beyond 127.0.0.1
	HostServices  Kind = "host-services"  // extra_hosts points at host-gateway
	HostFolder    Kind = "host-folder"    // a host path is mounted, directly or through a volume's driver
	Engine        Kind = "engine"         // the container engine itself, or what else it runs
	SSHAgent      Kind = "ssh-agent"      // a build is given the user's SSH agent
	Device        Kind = "device"         // a host device
	Capabilities  Kind = "capabilities"   // cap_add
	Privileged    Kind = "privileged"     // privileged, unconfined, or a build's host network or entitlements
	HostNamespace Kind = "host-namespace" // the host's network, process, IPC, UTS, user or cgroup namespace
	// WorkingFolder is the folder a user runs a recipe's command in. Every
	// command has it, so a recipe never declares it; the user agrees to it.
	WorkingFolder Kind = "working-folder"
)

// Kinds lists every kind.
var Kinds = []Kind{Internet, LAN, HostServices, HostFolder, Engine, SSHAgent, Device, Capabilities, Privileged, HostNamespace, WorkingFolder}

// Gated reports whether a kind needs a maintainer's review before a recipe
// that declares it is verified for the catalogue.
func (k Kind) Gated() bool { return k != Internet }

// Grant is one thing a resolved model gives a service beyond the sandbox.
type Grant struct {
	Kind    Kind   `json:"kind"`
	Service string `json:"service"`
	What    string `json:"what"` // in plain words, with the exact path, port or name
}

func (g Grant) String() string { return "services." + g.Service + " " + g.What }

// Grants reads every grant the resolved model makes, for a project whose
// workspace is ws as Compose was given it. A bind mount of ./inputs, ./outputs
// or ./recipe is the sandbox itself and grants nothing.
func Grants(m compose.Model, ws string) []Grant {
	var out []Grant
	for _, name := range sortedKeys(m.Services) {
		s := m.Services[name]
		add := func(k Kind, format string, args ...any) {
			out = append(out, Grant{Kind: k, Service: name, What: fmt.Sprintf(format, args...)})
		}
		if internet(m, name, map[string]bool{}) {
			add(Internet, "reaches the internet")
		}
		for _, p := range s.Ports {
			if !loopback(p.HostIP) {
				add(LAN, "publishes port %d to the local network", p.Target)
			}
		}
		for _, h := range s.ExtraHosts {
			if strings.HasSuffix(h, "=host-gateway") {
				add(HostServices, "reaches this machine's own services as %s", strings.TrimSuffix(h, "=host-gateway"))
			}
		}
		for _, mt := range s.Mounts {
			mount(add, m, mt, ws)
		}
		if s.APISocket {
			add(Engine, "controls the container engine through the socket use_api_socket mounts")
		}
		for _, from := range s.VolumesFrom {
			if c, ok := strings.CutPrefix(from, "container:"); ok {
				add(Engine, "mounts the volumes of the container %s, outside this recipe", c)
			}
		}
		if s.Privileged {
			add(Privileged, "runs privileged")
		}
		if s.HookPrivileged {
			add(Privileged, "runs a post_start or pre_stop hook privileged")
		}
		for _, o := range s.SecurityOpt {
			if unconfined(o) {
				add(Privileged, "turns off %s", o)
			}
		}
		if len(s.CapAdd) > 0 {
			add(Capabilities, "adds the capabilities %s", strings.Join(s.CapAdd, ", "))
		}
		for _, d := range s.Devices {
			add(Device, "uses the device %s", d)
		}
		for _, r := range s.DeviceRules {
			add(Device, "may use devices matching %s", r)
		}
		namespaces := []struct{ what, value string }{
			{"network", s.NetworkMode}, {"process", s.PID}, {"IPC", s.IPC}, {"UTS", s.UTS}, {"user", s.Userns}, {"cgroup", s.Cgroup},
		}
		for _, ns := range namespaces {
			switch {
			case ns.value == "host":
				add(HostNamespace, "shares this machine's %s namespace", ns.what)
			case strings.HasPrefix(ns.value, "container:"):
				add(Engine, "joins the %s namespace of the container %s, outside this recipe", ns.what, strings.TrimPrefix(ns.value, "container:"))
			}
		}
		for _, net := range s.Networks {
			if n := m.Networks[net]; n.External || foreign(m, n.Name, net) {
				add(Engine, "joins the existing network %s, outside this recipe", named(n.Name, net))
			}
		}
		if b := s.Build; b != nil {
			if len(b.SSH) > 0 {
				add(SSHAgent, "builds with your SSH agent (%s)", strings.Join(b.SSH, ", "))
			}
			if b.Network == "host" {
				add(Privileged, "builds on this machine's network")
			}
			if len(b.Entitlements) > 0 {
				add(Privileged, "builds with the entitlements %s", strings.Join(b.Entitlements, ", "))
			}
			if b.Privileged {
				add(Privileged, "builds privileged")
			}
			for _, h := range b.ExtraHosts {
				if strings.HasSuffix(h, "=host-gateway") {
					add(HostServices, "builds reaching this machine's own services as %s", strings.TrimSuffix(h, "=host-gateway"))
				}
			}
			for _, name := range sortedKeys(b.Contexts) {
				if c := slash(b.Contexts[name]); localContext(c) && !inWorkspace(c, slash(ws)) {
					add(HostFolder, "builds with the folder %s as its context %s", b.Contexts[name], name)
				}
			}
		}
	}
	return out
}

// mount reads the grants of one mount.
func mount(add func(Kind, string, ...any), m compose.Model, mt compose.Mount, ws string) {
	ro := ""
	if mt.ReadOnly {
		ro = ", read-only"
	}
	switch mt.Type {
	case "bind":
		source := slash(mt.Source)
		switch base := path.Base(source); {
		case base == "docker.sock" || base == "podman.sock":
			add(Engine, "controls the container engine through %s", mt.Source)
		case inWorkspace(source, slash(ws)):
			// the sandbox's own folders, or (refused by Problems) another workspace folder
		default:
			add(HostFolder, "mounts %s at %s%s", mt.Source, mt.Target, ro)
		}
	case "volume":
		v, declared := m.Volumes[mt.Source]
		switch {
		case !declared:
			// an anonymous volume
		case v.External || foreign(m, v.Name, mt.Source):
			add(Engine, "mounts the existing volume %s, outside this recipe", named(v.Name, mt.Source))
		case v.DriverOpts["device"] != "":
			add(HostFolder, "mounts %s at %s through the volume %s%s", v.DriverOpts["device"], mt.Target, mt.Source, ro)
		case len(v.DriverOpts) > 0 || (v.Driver != "" && v.Driver != "local"):
			add(HostFolder, "mounts storage outside the engine at %s through the volume %s%s", mt.Target, mt.Source, ro)
		}
	case "npipe":
		add(Engine, "opens the named pipe %s", mt.Source)
	}
}

// Check compares a recipe's declared access with what its resolved model
// grants, for the workspace ws. It returns one message per problem: a key UGT
// cannot say the access of, a grant whose kind is not declared (with the entry
// to add), a declared kind nothing grants, a kind declared twice, a bind mount
// or build context of a workspace folder that is not the sandbox's own, a
// recipe folder mounted writable, and output sent anywhere but the engine's
// own logs.
func Check(declared []recipe.Access, m compose.Model, ws string) []string {
	var problems []string
	for _, k := range m.Unknown {
		problems = append(problems, fmt.Sprintf("compose.yaml sets %s, which UGT cannot yet say the access of: remove it", k))
	}
	have := map[Kind]bool{}
	for _, d := range declared {
		k := Kind(d.Kind)
		if have[k] {
			problems = append(problems, fmt.Sprintf("access declares %s twice", k))
		}
		have[k] = true
	}
	granted := map[Kind][]Grant{}
	for _, g := range Grants(m, ws) {
		granted[g.Kind] = append(granted[g.Kind], g)
	}
	for _, k := range Kinds {
		gs := granted[k]
		switch {
		case len(gs) > 0 && !have[k]:
			problems = append(problems, fmt.Sprintf("%s: declare it in recipe.json's access as {\"kind\": %q, \"why\": \"...\"}, saying why the recipe needs it", gs[0], k))
		case len(gs) == 0 && have[k]:
			problems = append(problems, fmt.Sprintf("access declares %s, but nothing in compose.yaml needs it: remove it", k))
		}
	}
	for _, name := range sortedKeys(m.Services) {
		s := m.Services[name]
		for _, mt := range s.Mounts {
			source := slash(mt.Source)
			switch {
			case mt.Type != "bind" || !inWorkspace(source, slash(ws)):
			case !sandbox(source, slash(ws)):
				problems = append(problems, fmt.Sprintf("services.%s mounts %s, a folder of the workspace: keep data in a named volume, and mount only ./inputs and ./outputs whole, and ./recipe or a folder in it", name, mt.Source))
			case inFolder(source, slash(ws), workspace.RecipeDir) && !mt.ReadOnly:
				problems = append(problems, fmt.Sprintf("services.%s mounts %s writable: mount the recipe's own files read-only (:ro), so nothing that runs can change what runs next", name, mt.Source))
			}
		}
		if b := s.Build; b != nil {
			for _, ctx := range sortedKeys(b.Contexts) {
				if c := slash(b.Contexts[ctx]); localContext(c) && inWorkspace(c, slash(ws)) && !inFolder(c, slash(ws), workspace.RecipeDir) {
					problems = append(problems, fmt.Sprintf("services.%s builds with %s, a folder of the workspace: a build reads only ./recipe or a remote source", name, b.Contexts[ctx]))
				}
			}
		}
		if d := s.LogDriver; d != "" && d != "json-file" && d != "local" && d != "none" {
			problems = append(problems, fmt.Sprintf("services.%s sends its output through the log driver %s: UGT reads what a recipe prints itself, so remove logging.driver or use json-file or local", name, d))
		}
	}
	return problems
}

// KindsOf returns the kinds of grants, without repeats, in the order Kinds
// lists them.
func KindsOf(grants []Grant) []Kind {
	seen := map[Kind]bool{}
	for _, g := range grants {
		seen[g.Kind] = true
	}
	var out []Kind
	for _, k := range Kinds {
		if seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// In returns the grants made to the given services.
func In(grants []Grant, services []string) []Grant {
	var out []Grant
	for _, g := range grants {
		if slices.Contains(services, g.Service) {
			out = append(out, g)
		}
	}
	return out
}

// Digest identifies exactly what grants make: the user agrees to a recipe's
// access once, and again whenever it changes.
func Digest(grants []Grant) string {
	lines := make([]string, len(grants))
	for i, g := range grants {
		lines[i] = string(g.Kind) + "\x00" + g.Service + "\x00" + g.What
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// headings say what each kind lets a recipe do, for the person agreeing.
var headings = map[Kind]string{
	Internet:      "Reach the internet",
	LAN:           "Be reachable from your local network",
	HostServices:  "Reach services running on this computer",
	HostFolder:    "Use folders on this computer",
	Engine:        "Control the container engine",
	SSHAgent:      "Use your SSH keys while it builds",
	Device:        "Use devices attached to this computer",
	Capabilities:  "Have extra system capabilities",
	Privileged:    "Run with full privileges",
	HostNamespace: "Share parts of this computer's own system",
	WorkingFolder: "Read and change the files in the folder you run its commands in",
}

// Heading is what a kind of access lets a recipe do, in plain words.
func Heading(k Kind) string { return headings[k] }

// Commands are the grants of the commands a recipe provides: each works on
// the folder the user runs it in.
func Commands(p recipe.Provides) []Grant {
	var out []Grant
	for _, c := range p.Commands {
		out = append(out, Grant{Kind: WorkingFolder, Service: c.Service, What: "runs as the command " + c.Name + " in the folder you run it in"})
	}
	return out
}

// Describe is what the user reads before agreeing to a recipe's grants: each
// kind in plain words, exactly what it grants, and why the recipe says it
// needs it.
func Describe(id string, declared []recipe.Access, grants []Grant) string {
	why := map[Kind]string{}
	for _, d := range declared {
		why[Kind(d.Kind)] = d.Why
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s asks for access beyond UGT's sandbox:\n", id)
	for _, k := range KindsOf(grants) {
		fmt.Fprintf(&b, "\n%s\n", headings[k])
		for _, g := range grants {
			if g.Kind == k {
				fmt.Fprintf(&b, "  %s\n", g)
			}
		}
		if w := why[k]; w != "" {
			fmt.Fprintf(&b, "  The recipe says: %s\n", w)
		}
	}
	b.WriteString("\nAllow it?")
	return b.String()
}

// internet reports whether a service can reach the internet: it has a network
// that is not internal, the host's, or another service's that does.
func internet(m compose.Model, name string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true
	s := m.Services[name]
	switch mode := s.NetworkMode; {
	case mode == "none":
		return false
	case strings.HasPrefix(mode, "service:"):
		return internet(m, strings.TrimPrefix(mode, "service:"), seen)
	case mode != "": // host, bridge, default, or another container's
		return true
	}
	if len(s.Networks) == 0 {
		return !m.Networks["default"].Internal
	}
	for _, net := range s.Networks {
		if !m.Networks[net].Internal {
			return true
		}
	}
	return false
}

func loopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}

// unconfined reports whether a security option turns a confinement off.
func unconfined(o string) bool {
	o = strings.ToLower(strings.ReplaceAll(o, "=", ":"))
	return strings.HasSuffix(o, ":unconfined") || o == "label:disable"
}

func inWorkspace(source, ws string) bool {
	return source == ws || strings.HasPrefix(source, strings.TrimSuffix(ws, "/")+"/")
}

// localContext reports whether a build context is a folder on disk, not a
// remote source, an image or another service's build.
func localContext(c string) bool {
	for _, remote := range []string{"https://", "http://", "git@", "git://", "docker-image://", "oci-layout://", "service:", "target:"} {
		if strings.HasPrefix(c, remote) {
			return false
		}
	}
	return true
}

// inFolder reports whether a path is the workspace folder dir or inside it.
func inFolder(source, ws, dir string) bool {
	rel := strings.TrimPrefix(source, strings.TrimSuffix(ws, "/")+"/")
	return rel == dir || strings.HasPrefix(rel, dir+"/")
}

// sandbox reports whether a bind source is one of the workspace folders a
// recipe may mount: inputs or outputs whole, or recipe or a folder in it.
func sandbox(source, ws string) bool {
	rel := strings.TrimPrefix(source, strings.TrimSuffix(ws, "/")+"/")
	return slices.Contains(recipe.Mountable, rel) || inFolder(source, ws, workspace.RecipeDir)
}

func slash(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// foreign reports whether a top-level network's or volume's engine name is not
// the one Compose gives this project's own: a name a recipe sets reuses
// whatever on the engine already has it, such as another recipe's data.
func foreign(m compose.Model, name, key string) bool {
	return m.Project != "" && name != "" && name != m.Project+"_"+key
}

// named is a top-level network's or volume's engine name, or its key.
func named(name, key string) string {
	if name != "" {
		return name
	}
	return key
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
