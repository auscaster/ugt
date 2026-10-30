// Package report is the verification report (spec/report/v0): the one result
// that CLI users, agents through MCP, and catalogue CI all read.
package report

import (
	"time"

	"github.com/auscaster/ugt/internal/domain/diagnose"
)

// Format names the report contract.
const Format = "ugt-report-v0"

// Steps of a verification, in order.
const (
	StepBuild       = "build"
	StepPull        = "pull"
	StepSample      = "sample"
	StepTask        = "task"
	StepCheck       = "check"
	StepSampleCheck = "sample-check"
	StepStrength    = "check-strength"
)

// Report is the outcome of one verification.
type Report struct {
	Format    string    `json:"report_format"`
	Recipe    string    `json:"recipe"`
	Revision  string    `json:"revision"`
	Arch      string    `json:"arch"`
	Started   time.Time `json:"started"`
	Seconds   float64   `json:"seconds"`
	Passed    bool      `json:"passed"`
	Steps     []Step    `json:"steps"`
	Strength  []Probe   `json:"check_strength,omitempty"`
	Workspace string    `json:"workspace"`
}

// Step is one step's result. Log holds the tail of its output when it failed.
type Step struct {
	Name     string             `json:"name"`
	Exit     int                `json:"exit"`
	Seconds  float64            `json:"seconds"`
	Log      string             `json:"log,omitempty"`
	Findings []diagnose.Finding `json:"findings,omitempty"`
}

// Probe is one check run against deliberately broken outputs; Rejected must
// be true for the recipe to pass.
type Probe struct {
	Mutation string `json:"mutation"`
	Check    string `json:"check"`
	Rejected bool   `json:"rejected"`
}

// FailedStep returns the first failed step, if any.
func (r Report) FailedStep() (Step, bool) {
	for _, s := range r.Steps {
		if s.Exit != 0 {
			return s, true
		}
	}
	return Step{}, false
}

// WeakChecks returns the probes a check failed to reject.
func (r Report) WeakChecks() []Probe {
	var weak []Probe
	for _, p := range r.Strength {
		if !p.Rejected {
			weak = append(weak, p)
		}
	}
	return weak
}
