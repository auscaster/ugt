package app

import (
	"context"
	"io"
	"testing"

	"github.com/usegeekthing/ugt/internal/domain/catalogue"
)

// sentRuns records the run reports UGT sends.
type sentRuns []catalogue.Run

func (s *sentRuns) Report(_ context.Context, r catalogue.Run) error {
	*s = append(*s, r)
	return nil
}

// UGT asks once, the first time a catalogue recipe fails here. A yes reports
// that run and every catalogue run after it, as identifiers only; a no sends
// nothing and is not asked again. A recipe made here is never reported.
func TestRunReports(t *testing.T) {
	for _, c := range []struct {
		agree bool
		local bool
	}{{agree: true}, {agree: false}, {agree: true, local: true}} {
		engine := &fakeEngine{config: jobConfig(), services: services(true)}
		works := engine.services["task"]
		engine.services["task"] = func(_ string, w io.Writer) int {
			_, _ = io.WriteString(w, "Killed\n")
			return 137
		}
		a, _, home := runApp(t, engine)
		if c.local {
			a.Catalogues[0].(*fakeCatalogue).source = catalogue.Local
		}
		p, sent := &askingPrompt{agree: c.agree}, &sentRuns{}
		a.Values, a.Prompt, a.Reports, a.Version = memValues{}, p, sent, "1.2.3"
		run := func() RunResult {
			res, err := a.Run(context.Background(), RunOptions{Project: "example/tool", Inputs: []string{userFile(t, home, "r.docx")}})
			if err != nil {
				t.Fatal(err)
			}
			return res
		}
		if run().Passed {
			t.Fatal("the run was meant to fail")
		}
		engine.services["task"] = works
		if !run().Passed {
			t.Fatal("the run was meant to pass")
		}
		if want := map[bool]int{true: 0, false: 1}[c.local]; len(p.asked) != want {
			t.Fatalf("%+v: asked %d times", c, len(p.asked))
		}
		if !c.agree || c.local {
			if len(*sent) != 0 {
				t.Errorf("%+v: sent %+v", c, *sent)
			}
			continue
		}
		if len(*sent) != 2 {
			t.Fatalf("sent %+v", *sent)
		}
		failed, passed := (*sent)[0], (*sent)[1]
		if failed.Format != catalogue.RunFormat || failed.Recipe != "example/tool/markdown" || failed.Outcome != "failed" ||
			failed.Step != "task" || failed.Diagnosis != "memory" || failed.Arch != "arm64" || failed.UGT != "1.2.3" ||
			passed.Outcome != "passed" || passed.Step != "" || passed.Install != failed.Install || passed.ID == failed.ID {
			t.Errorf("sent %+v", *sent)
		}
	}
}
