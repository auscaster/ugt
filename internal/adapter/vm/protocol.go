//go:build darwin || linux

package vm

// Every ugt reaches the background process on the machine's socket: one line
// of JSON each way, after which an ssh request's connection carries the SSH
// protocol to the machine's sshd.

type request struct {
	Op   string `json:"op"`
	Path string `json:"path,omitempty"` // a folder on the Mac, to share or release
}

type response struct {
	Revision string `json:"revision,omitempty"` // the layer the machine runs
	Error    string `json:"error,omitempty"`
}

const (
	opSSH     = "ssh"     // a connection to the machine's sshd
	opStatus  = "status"  // the layer it runs
	opFolder  = "folder"  // share a folder at its own path
	opRelease = "release" // stop sharing it
	opStop    = "stop"    // power off; answered once the machine has stopped
)

// readyLine is what the background process tells the ugt that started it
// once the machine's engine answers; anything else is why it could not start.
const readyLine = "ready"
