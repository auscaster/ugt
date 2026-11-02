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

// What Compose 5 prints for keys that reach past the sandbox in ways older
// models never read, captured from `docker compose config --format json` on 2
// November 2026.
const unread = `{"name": "probe", "networks": {"default": {"name": "probe_default", "ipam": {}}},
 "services": {
  "api": {"command": null, "entrypoint": null, "provider": {"type": "sh", "options": {"c": ["id"]}}, "image": "y"},
  "task": {"build": {"context": "/ws/recipe", "dockerfile": "Dockerfile", "cache_to": ["type=local,dest=/tmp/cache"],
    "additional_contexts": {"base": "docker-image://alpine", "host": "/", "lib": "/ws/recipe/lib"}},
   "command": null, "entrypoint": null, "image": "x",
   "logging": {"driver": "syslog", "options": {"syslog-address": "udp://1.2.3.4:514"}},
   "networks": {"default": null}, "runtime": "runc", "use_api_socket": true}},
 "x-top": 1}`

func TestParseNamesWhatItCannotRead(t *testing.T) {
	m, err := Parse([]byte(unread))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"services.api.provider", "services.task.build.cache_to", "services.task.runtime"}; !slices.Equal(m.Unknown, want) {
		t.Errorf("unknown %v, want %v", m.Unknown, want)
	}
	task := m.Services["task"]
	if !task.APISocket || task.LogDriver != "syslog" || task.Build.Contexts["host"] != "/" || len(task.Build.Contexts) != 3 {
		t.Errorf("task %+v, build %+v", task, task.Build)
	}
	// a list of name=value, as older versions print it
	m, err = Parse([]byte(`{"services": {"s": {"image": "x", "build": {"context": "/ws/recipe", "additional_contexts": ["host=/"]}}}}`))
	if err != nil || m.Services["s"].Build.Contexts["host"] != "/" {
		t.Errorf("contexts %+v, %v", m.Services["s"].Build, err)
	}
}

// Captured from `docker compose config --format json` on 2 November 2026: a
// privileged hook, and a volume and a network named as another project's.
const named = `{"name": "probe",
 "networks": {"default": {"name": "probe_default", "ipam": {}}, "shared": {"name": "other-recipe_default", "ipam": {}}},
 "volumes": {"data": {"name": "other-recipe_data"}, "own": {"name": "probe_own"}},
 "services": {"app": {"image": "x", "networks": {"default": null, "shared": null},
  "post_start": [{"command": ["sh", "-c", "id"], "privileged": true}], "pre_stop": [{"command": ["true"]}],
  "volumes": [{"type": "volume", "source": "data", "target": "/data", "volume": {}}, {"type": "volume", "source": "own", "target": "/own", "volume": {}}]}}}`

func TestParseHooksAndNames(t *testing.T) {
	m, err := Parse([]byte(named))
	if err != nil {
		t.Fatal(err)
	}
	if m.Project != "probe" || !m.Services["app"].HookPrivileged || m.Volumes["data"].Name != "other-recipe_data" || m.Networks["shared"].Name != "other-recipe_default" || len(m.Unknown) != 0 {
		t.Errorf("model %+v", m)
	}
}
