package link

import "testing"

// A link names a project, or one of its recipes, and nothing else.
func TestParse(t *testing.T) {
	for in, want := range map[string]Link{
		"ugt://run/openai/whisper":            {Project: "openai/whisper"},
		"ugt://run/openai/whisper/subtitles":  {Project: "openai/whisper", Task: "subtitles"},
		"ugt://run/OpenAI/Whisper/subtitles/": {Project: "openai/whisper", Task: "subtitles"},
	} {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("%s: %+v, %v", in, got, err)
		}
		if again, _ := Parse(got.String()); again != got {
			t.Errorf("%s does not read back: %s", in, got.String())
		}
	}
	for _, in := range []string{
		"https://run/openai/whisper",
		"ugt://exec/openai/whisper",
		"ugt://run/openai",
		"ugt://run/openai/whisper/subtitles/more",
		"ugt://run/openai/whisper/Not A Task",
		"ugt://run/openai/whisper/subtitles?input=/etc/passwd",
		"ugt://run/openai/whisper#x",
		"ugt://me@run/openai/whisper",
		"ugt://run/../whisper",
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("%s read as %+v", in, got)
		}
	}
}
