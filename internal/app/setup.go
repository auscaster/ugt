package app

import (
	"context"
	"fmt"

	"github.com/auscaster/ugt/internal/domain/compose"
	"github.com/auscaster/ugt/internal/domain/ledger"
)

// Setup makes this machine ready to run recipes: on Windows it installs WSL
// when allowed and sets up UGT's own engine in it; elsewhere it uses the
// engine already there. What it creates goes in the ledger.
func (a *App) Setup(ctx context.Context, opts SetupOptions) (SetupResult, error) {
	res, err := a.Runtime.Setup(ctx, opts)
	if err != nil {
		return res, err
	}
	if res.Created != "" {
		_ = a.record(ledger.Entry{Kind: ledger.Runtime, Name: res.Created})
	}
	return res, nil
}

// Check is one thing ugt doctor looked at.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// DoctorReport is everything ugt doctor looked at.
type DoctorReport struct {
	Runtime string  `json:"runtime"`
	OK      bool    `json:"ok"`
	Checks  []Check `json:"checks"`
}

// minMemory is what the catalogue's larger recipes reserve.
const minMemory = 4 << 30

// Doctor names the runtime, the engine, its memory and the network UGT
// needs, each with the exact fix when something is wrong.
func (a *App) Doctor(ctx context.Context) (DoctorReport, error) {
	rep := DoctorReport{Runtime: a.Runtime.Name(), OK: true}
	add := func(c Check) {
		rep.Checks = append(rep.Checks, c)
		rep.OK = rep.OK && c.OK
	}
	status, err := a.Runtime.Status(ctx)
	if err != nil {
		return rep, err
	}
	if !status.Ready {
		add(Check{Name: "engine", Detail: status.Problem, Fix: status.Fix})
		return rep, nil
	}
	info, err := a.Engine.Info(ctx)
	if err != nil {
		add(Check{Name: "engine", Detail: "the engine is up but does not answer: " + err.Error(), Fix: "Run ugt setup to repair it."})
		return rep, nil
	}
	add(Check{Name: "engine", OK: true, Detail: fmt.Sprintf("Docker Engine %s on linux/%s, in the %s runtime", info.Version, info.Arch, rep.Runtime)})
	memory := Check{Name: "memory", OK: info.MemoryBytes == 0 || info.MemoryBytes >= minMemory,
		Detail: fmt.Sprintf("the engine has %.1f GB", float64(info.MemoryBytes)/(1<<30))}
	if !memory.OK {
		memory.Fix = "Larger recipes reserve 3 to 4 GB. Give the engine more memory: Docker Desktop's Settings, Resources; for WSL, memory= in %UserProfile%\\.wslconfig, then wsl --shutdown."
	}
	add(memory)
	if _, err := a.Engine.ImageDigest(ctx, compose.ProbeImage); err != nil {
		add(Check{Name: "registry", Detail: "the engine cannot reach Docker Hub: " + err.Error(),
			Fix: "Check this machine's internet connection, proxy or firewall for registry-1.docker.io."})
	} else {
		add(Check{Name: "registry", OK: true, Detail: "the engine reaches Docker Hub"})
	}
	if a.GitHub != nil {
		if _, err := a.GitHub.Repo(ctx, "openai", "whisper"); err != nil {
			add(Check{Name: "github", Detail: "GitHub does not answer: " + err.Error(), Fix: "Check the connection to api.github.com; set GITHUB_TOKEN if the rate limit is the problem."})
		} else {
			add(Check{Name: "github", OK: true, Detail: "GitHub answers"})
		}
	}
	return rep, nil
}
