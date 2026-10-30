// Package placement decides whether a recipe can run where it is asked to,
// from what its compose.yaml reserves and what the runtime has, and says why
// in plain words when it cannot.
package placement

import "fmt"

// Needs is what a recipe reserves when its default services run.
type Needs struct {
	MemoryBytes int64 // the sum of their memory limits
	GPU         bool  // one of them reserves a GPU
}

// Host is what the runtime offers.
type Host struct {
	Runtime     string // wsl, vm or host
	MemoryBytes int64  // the engine's memory; 0 when unknown
	GPU         bool   // a GPU containers can use
}

// Decision is whether to run, and if not, why and how to fix it.
type Decision struct {
	Run    bool   `json:"run"`
	Reason string `json:"reason,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// Decide compares a recipe's needs with the host.
func Decide(n Needs, h Host) Decision {
	if n.GPU && !h.GPU {
		return Decision{
			Reason: "This recipe needs a GPU that containers can use, and this machine's engine has none.",
			Fix:    "Run it on a machine with an NVIDIA GPU and its container toolkit, or ask for a recipe that runs on the CPU.",
		}
	}
	if h.MemoryBytes > 0 && n.MemoryBytes > h.MemoryBytes {
		fix := "Give the container engine more memory, then try again."
		switch h.Runtime {
		case "wsl":
			fix = "Give WSL more memory: set memory= in %UserProfile%\\.wslconfig under [wsl2], run wsl --shutdown, then try again."
		case "host":
			fix = "Give the container engine more memory (Docker Desktop: Settings, Resources), then try again."
		}
		return Decision{
			Reason: fmt.Sprintf("This recipe reserves %s of memory and the engine has %s.", gb(n.MemoryBytes), gb(h.MemoryBytes)),
			Fix:    fix,
		}
	}
	return Decision{Run: true}
}

func gb(n int64) string { return fmt.Sprintf("%.1f GB", float64(n)/(1<<30)) }
