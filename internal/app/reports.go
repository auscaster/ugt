package app

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/auscaster/ugt/internal/domain/catalogue"
)

// The user's answer about run reports, and this machine's random ID, kept with
// UGT's own settings.
const (
	ReportsSetting = "run-reports" // "on" or "off"; unset until the user is asked
	installSetting = "install"
)

// reportRun tells the catalogue how a run of one of its recipes went, when the
// user agreed. UGT asks once, through its own prompt, the first time one of
// them fails here, so nothing about any run is sent before the user says yes.
// A report never changes the run's result.
func (a *App) reportRun(ctx context.Context, res RunResult, arch string) {
	if a.Reports == nil || a.Values == nil || len(res.Steps) == 0 {
		return
	}
	prefs, err := a.Values.Get(LoginRecipe)
	if err != nil {
		return
	}
	agreed := prefs[ReportsSetting]
	if agreed == "" && !res.Passed && a.Prompt != nil {
		yes, err := a.Prompt.Confirm(ctx, Question{
			Title: "Help keep UGT's recipes working?",
			Message: fmt.Sprintf("%s did not work on this machine. UGT can tell its catalogue how each run of a catalogue recipe goes, so broken recipes get fixed: "+
				"the recipe and its revision, whether it worked, the step that failed and what UGT diagnosed, this machine's architecture, UGT's version, "+
				"and a random ID for this machine. Never your files, paths, output or anything you typed. Change it any time with: ugt reports off", res.Recipe),
			Yes: "Send reports",
			No:  "Don't send",
		})
		if err != nil {
			return // nothing can ask here: ask another time
		}
		agreed = map[bool]string{true: "on", false: "off"}[yes]
		if a.Values.Set(LoginRecipe, ReportsSetting, agreed) != nil {
			return
		}
	}
	if agreed != "on" {
		return
	}
	install := prefs[installSetting]
	if install == "" {
		install = newID()
		if a.Values.Set(LoginRecipe, installSetting, install) != nil {
			return
		}
	}
	r := catalogue.Run{Format: catalogue.RunFormat, ID: newID(), Install: install, Recipe: res.Recipe, Revision: res.Revision,
		Outcome: "passed", Arch: arch, UGT: a.Version}
	if !res.Passed {
		r.Outcome = "failed"
		if last := res.Steps[len(res.Steps)-1]; last.Exit != 0 {
			r.Step = last.Name
			if len(last.Findings) > 0 {
				r.Diagnosis = last.Findings[0].Rule
			}
		}
	}
	_ = a.Reports.Report(ctx, r)
}

// Reporting says whether this machine reports its catalogue runs: "on", "off",
// or "" before the user was asked.
func (a *App) Reporting() (string, error) {
	prefs, err := a.Values.Get(LoginRecipe)
	return prefs[ReportsSetting], err
}

// SetReporting records the user's answer about run reports.
func (a *App) SetReporting(on bool) error {
	return a.Values.Set(LoginRecipe, ReportsSetting, map[bool]string{true: "on", false: "off"}[on])
}

// newID is a random version 4 UUID.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
