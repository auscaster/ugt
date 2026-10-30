package compose

import (
	"slices"
	"testing"
)

func TestParseResources(t *testing.T) {
	m, err := Parse([]byte(`{"services": {
		"task": {"image": "ugt/a/b/c:1", "build": {"context": "/ws/recipe"}, "mem_limit": 3221225472, "cpus": 4},
		"check": {"image": "ugt/a/b/c:1", "profiles": ["check"]},
		"api": {"image": "x", "ports": [{"mode": "ingress", "host_ip": "127.0.0.1", "target": 3002, "protocol": "tcp"}],
			"deploy": {"resources": {"limits": {"memory": "1073741824"},
				"reservations": {"devices": [{"capabilities": ["gpu"]}]}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !m.Services["task"].Builds() || m.Services["check"].Builds() || m.Services["check"].Profiles[0] != "check" {
		t.Errorf("model %+v", m)
	}
	if task, api := m.Services["task"], m.Services["api"]; task.MemoryBytes != 3<<30 || api.MemoryBytes != 1<<30 || task.CPUs != 4 ||
		len(api.Ports) != 1 || api.Ports[0] != (Port{Target: 3002, HostIP: "127.0.0.1"}) || !api.GPU || task.GPU {
		t.Errorf("resources: task %+v, api %+v", task, api)
	}
}

// What Compose 5 prints for a service that reaches beyond the sandbox, as
// captured from `docker compose config --format json` on 30 October 2026.
const reaching = `{"name": "probe",
 "networks": {"default": {"name": "probe_default", "ipam": {}}, "inside": {"name": "probe_inside", "ipam": {}, "internal": true}},
 "services": {
  "task": {
   "build": {"context": "/ws/recipe", "dockerfile": "Dockerfile", "entitlements": ["network.host"], "ssh": ["default"], "network": "host", "privileged": true},
   "cap_add": ["NET_ADMIN"], "cgroup": "host",
   "devices": [{"source": "/dev/ttyUSB0", "target": "/dev/ttyUSB0", "permissions": "rwm"}],
   "extra_hosts": ["host.docker.internal=host-gateway", "x=1.2.3.4"],
   "image": "ugt/x:1", "ipc": "host", "network_mode": "host", "pid": "host", "privileged": true,
   "security_opt": ["seccomp:unconfined", "apparmor=unconfined"], "userns_mode": "host", "uts": "host",
   "volumes": [
    {"type": "bind", "source": "/ws/inputs", "target": "/work/inputs", "read_only": true, "bind": {}},
    {"type": "volume", "source": "home", "target": "/host", "volume": {}},
    {"type": "bind", "source": "/var/run/docker.sock", "target": "/var/run/docker.sock", "bind": {}},
    {"type": "tmpfs", "target": "/tmp"}]},
  "web": {"image": "nginx", "networks": {"default": null, "inside": null},
   "ports": [{"mode": "ingress", "target": 80, "published": "8080", "protocol": "tcp"}]}},
 "volumes": {"home": {"name": "probe_home", "driver_opts": {"device": "/Users", "o": "bind", "type": "none"}}}}`

func TestParseReach(t *testing.T) {
	m, err := Parse([]byte(reaching))
	if err != nil {
		t.Fatal(err)
	}
	task, web := m.Services["task"], m.Services["web"]
	b := task.Build
	if b == nil || !slices.Equal(b.SSH, []string{"default"}) || b.Network != "host" || !b.Privileged || len(b.Entitlements) != 1 {
		t.Errorf("build %+v", b)
	}
	if !task.Privileged || task.NetworkMode != "host" || task.PID != "host" || task.IPC != "host" || task.UTS != "host" ||
		task.Userns != "host" || task.Cgroup != "host" || !slices.Equal(task.CapAdd, []string{"NET_ADMIN"}) ||
		!slices.Equal(task.Devices, []string{"/dev/ttyUSB0"}) || len(task.SecurityOpt) != 2 {
		t.Errorf("task %+v", task)
	}
	if !slices.Equal(task.ExtraHosts, []string{"host.docker.internal=host-gateway", "x=1.2.3.4"}) {
		t.Errorf("extra_hosts %v", task.ExtraHosts)
	}
	if len(task.Mounts) != 4 || task.Mounts[0] != (Mount{Type: "bind", Source: "/ws/inputs", Target: "/work/inputs", ReadOnly: true}) ||
		task.Mounts[1].Type != "volume" || task.Mounts[1].Source != "home" {
		t.Errorf("mounts %+v", task.Mounts)
	}
	if !slices.Equal(web.Networks, []string{"default", "inside"}) || web.Ports[0] != (Port{Target: 80, Published: "8080"}) {
		t.Errorf("web %+v", web)
	}
	if !m.Networks["inside"].Internal || m.Networks["default"].Internal {
		t.Errorf("networks %+v", m.Networks)
	}
	if v := m.Volumes["home"]; v.DriverOpts["device"] != "/Users" || v.External {
		t.Errorf("volumes %+v", m.Volumes)
	}
}

// Older Compose versions print short device and extra_hosts forms.
func TestParseShortForms(t *testing.T) {
	m, err := Parse([]byte(`{"services": {"s": {"image": "x", "devices": ["/dev/kvm:/dev/kvm"], "extra_hosts": ["a:host-gateway"]}},
		"volumes": {"v": {"external": true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	s := m.Services["s"]
	if !slices.Equal(s.Devices, []string{"/dev/kvm"}) || !slices.Equal(s.ExtraHosts, []string{"a=host-gateway"}) || !m.Volumes["v"].External {
		t.Errorf("model %+v", m)
	}
}
