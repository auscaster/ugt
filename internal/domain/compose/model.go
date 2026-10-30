package compose

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Model is a resolved Compose configuration, as `docker compose config
// --format json` prints it: what will really run, with every variable and
// default filled in and every path made absolute. It is read once, here, for
// placement, ports, builds and the access a recipe is granted.
type Model struct {
	Services map[string]Service
	Networks map[string]Network
	Volumes  map[string]Volume
}

// Service is one resolved service.
type Service struct {
	Image       string
	Build       *Build // nil when the service does not build
	Profiles    []string
	SecurityOpt []string
	MemoryBytes int64   // its memory limit; 0 when it sets none
	CPUs        float64 // its CPU limit; 0 when it sets none
	Ports       []Port
	GPU         bool // it reserves a GPU
	NetworkMode string
	Networks    []string // the networks it joins, when NetworkMode is unset
	Privileged  bool
	CapAdd      []string
	Devices     []string // host device paths
	DeviceRules []string // device_cgroup_rules
	PID         string
	IPC         string
	UTS         string
	Userns      string
	Cgroup      string
	ExtraHosts  []string // as name=address
	Mounts      []Mount
	VolumesFrom []string
}

// Build is what a service's build is given beyond its context.
type Build struct {
	SSH          []string
	Network      string
	Entitlements []string
	Privileged   bool
	ExtraHosts   []string // as name=address
}

// Port is one published port.
type Port struct {
	Target    int
	HostIP    string // "" when published on every interface
	Published string
}

// Mount is one of a service's volumes, in Compose's long form.
type Mount struct {
	Type     string // bind, volume, tmpfs, npipe, cluster, image
	Source   string // an absolute host path for bind, a top-level volume's key for volume
	Target   string
	ReadOnly bool
}

// Network is a top-level network.
type Network struct {
	Name     string
	Driver   string
	Internal bool
	External bool
}

// Volume is a top-level volume.
type Volume struct {
	Name       string
	Driver     string
	DriverOpts map[string]string
	External   bool
}

// Builds reports whether the service builds its image.
func (s Service) Builds() bool { return s.Build != nil }

// Parse reads `docker compose config --format json`.
func Parse(data []byte) (Model, error) {
	var raw struct {
		Services map[string]rawService `json:"services"`
		Networks map[string]struct {
			Name     string          `json:"name"`
			Driver   string          `json:"driver"`
			Internal bool            `json:"internal"`
			External json.RawMessage `json:"external"`
		} `json:"networks"`
		Volumes map[string]struct {
			Name       string            `json:"name"`
			Driver     string            `json:"driver"`
			DriverOpts map[string]string `json:"driver_opts"`
			External   json.RawMessage   `json:"external"`
		} `json:"volumes"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Model{}, fmt.Errorf("read the resolved Compose model: %w", err)
	}
	m := Model{Services: map[string]Service{}, Networks: map[string]Network{}, Volumes: map[string]Volume{}}
	for name, s := range raw.Services {
		svc, err := s.service()
		if err != nil {
			return Model{}, fmt.Errorf("services.%s: %w", name, err)
		}
		m.Services[name] = svc
	}
	for key, n := range raw.Networks {
		m.Networks[key] = Network{Name: n.Name, Driver: n.Driver, Internal: n.Internal, External: isSet(n.External)}
	}
	for key, v := range raw.Volumes {
		m.Volumes[key] = Volume{Name: v.Name, Driver: v.Driver, DriverOpts: v.DriverOpts, External: isSet(v.External)}
	}
	return m, nil
}

type rawService struct {
	Image       string          `json:"image"`
	Build       json.RawMessage `json:"build"`
	Profiles    []string        `json:"profiles"`
	SecurityOpt []string        `json:"security_opt"`
	MemLimit    json.Number     `json:"mem_limit"`
	CPUs        json.Number     `json:"cpus"`
	Ports       []struct {
		Target    int    `json:"target"`
		HostIP    string `json:"host_ip"`
		Published string `json:"published"`
	} `json:"ports"`
	GPUs        json.RawMessage            `json:"gpus"`
	NetworkMode string                     `json:"network_mode"`
	Networks    map[string]json.RawMessage `json:"networks"`
	Privileged  bool                       `json:"privileged"`
	CapAdd      []string                   `json:"cap_add"`
	Devices     []json.RawMessage          `json:"devices"`
	DeviceRules []string                   `json:"device_cgroup_rules"`
	PID         string                     `json:"pid"`
	IPC         string                     `json:"ipc"`
	UTS         string                     `json:"uts"`
	Userns      string                     `json:"userns_mode"`
	Cgroup      string                     `json:"cgroup"`
	ExtraHosts  json.RawMessage            `json:"extra_hosts"`
	Volumes     []struct {
		Type     string `json:"type"`
		Source   string `json:"source"`
		Target   string `json:"target"`
		ReadOnly bool   `json:"read_only"`
	} `json:"volumes"`
	VolumesFrom []string `json:"volumes_from"`
	Deploy      struct {
		Resources struct {
			Limits struct {
				Memory json.Number `json:"memory"`
				CPUs   json.Number `json:"cpus"`
			} `json:"limits"`
			Reservations struct {
				Devices []struct {
					Capabilities []string `json:"capabilities"`
				} `json:"devices"`
			} `json:"reservations"`
		} `json:"resources"`
	} `json:"deploy"`
}

func (s rawService) service() (Service, error) {
	svc := Service{
		Image:       s.Image,
		Profiles:    s.Profiles,
		SecurityOpt: s.SecurityOpt,
		GPU:         isSet(s.GPUs),
		NetworkMode: s.NetworkMode,
		Privileged:  s.Privileged,
		CapAdd:      s.CapAdd,
		DeviceRules: s.DeviceRules,
		PID:         s.PID,
		IPC:         s.IPC,
		UTS:         s.UTS,
		Userns:      s.Userns,
		Cgroup:      s.Cgroup,
		VolumesFrom: s.VolumesFrom,
	}
	if isSet(s.Build) {
		var b struct {
			SSH          []string        `json:"ssh"`
			Network      string          `json:"network"`
			Entitlements []string        `json:"entitlements"`
			Privileged   bool            `json:"privileged"`
			ExtraHosts   json.RawMessage `json:"extra_hosts"`
		}
		if err := json.Unmarshal(s.Build, &b); err != nil {
			return Service{}, fmt.Errorf("build: %w", err)
		}
		hosts, err := extraHosts(b.ExtraHosts)
		if err != nil {
			return Service{}, fmt.Errorf("build.extra_hosts: %w", err)
		}
		svc.Build = &Build{SSH: b.SSH, Network: b.Network, Entitlements: b.Entitlements, Privileged: b.Privileged, ExtraHosts: hosts}
	}
	for _, limit := range []json.Number{s.MemLimit, s.Deploy.Resources.Limits.Memory} {
		if n, err := limit.Int64(); err == nil && (svc.MemoryBytes == 0 || n < svc.MemoryBytes) {
			svc.MemoryBytes = n
		}
	}
	for _, limit := range []json.Number{s.CPUs, s.Deploy.Resources.Limits.CPUs} {
		if n, err := limit.Float64(); err == nil && n > 0 && (svc.CPUs == 0 || n < svc.CPUs) {
			svc.CPUs = n
		}
	}
	for _, d := range s.Deploy.Resources.Reservations.Devices {
		svc.GPU = svc.GPU || slices.Contains(d.Capabilities, "gpu")
	}
	for _, p := range s.Ports {
		svc.Ports = append(svc.Ports, Port{Target: p.Target, HostIP: p.HostIP, Published: p.Published})
	}
	for name := range s.Networks {
		svc.Networks = append(svc.Networks, name)
	}
	slices.Sort(svc.Networks)
	for _, d := range s.Devices {
		source, err := deviceSource(d)
		if err != nil {
			return Service{}, fmt.Errorf("devices: %w", err)
		}
		svc.Devices = append(svc.Devices, source)
	}
	hosts, err := extraHosts(s.ExtraHosts)
	if err != nil {
		return Service{}, fmt.Errorf("extra_hosts: %w", err)
	}
	svc.ExtraHosts = hosts
	for _, v := range s.Volumes {
		svc.Mounts = append(svc.Mounts, Mount{Type: v.Type, Source: v.Source, Target: v.Target, ReadOnly: v.ReadOnly})
	}
	return svc, nil
}

// deviceSource is a device's host path, from the long form Compose prints or
// the short "host:container" form older versions print.
func deviceSource(raw json.RawMessage) (string, error) {
	var long struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(raw, &long); err == nil {
		return long.Source, nil
	}
	var short string
	if err := json.Unmarshal(raw, &short); err != nil {
		return "", err
	}
	source, _, _ := strings.Cut(short, ":")
	return source, nil
}

// extraHosts reads extra_hosts as name=address, from the list Compose prints
// (with = or :) or a map.
func extraHosts(raw json.RawMessage) ([]string, error) {
	if !isSet(raw) {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		out := make([]string, len(list))
		for i, h := range list {
			if name, addr, ok := strings.Cut(h, "="); ok {
				out[i] = name + "=" + addr
			} else if name, addr, ok := strings.Cut(h, ":"); ok {
				out[i] = name + "=" + addr
			} else {
				out[i] = h
			}
		}
		return out, nil
	}
	var byName map[string]any
	if err := json.Unmarshal(raw, &byName); err != nil {
		return nil, err
	}
	var out []string
	for name, addr := range byName {
		out = append(out, fmt.Sprintf("%s=%v", name, addr))
	}
	slices.Sort(out)
	return out, nil
}

// isSet reports whether a JSON value is present and neither null nor false.
func isSet(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "false"
}
