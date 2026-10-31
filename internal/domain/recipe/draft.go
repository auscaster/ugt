package recipe

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ScaffoldMark starts the message every placeholder service prints, so a
// verification report can say which service is still to be written.
const ScaffoldMark = "ugt scaffold:"

// What each conventional service must do. A new recipe's services print this
// and fail until their author writes the real command.
var placeholders = map[string]string{
	ServiceTask:        "write the task service: run the project on /work/inputs and write its results to /work/outputs",
	ServiceCheck:       "write the check service: fail unless outputs/ holds what the task made, for any input",
	ServiceSample:      "write the sample service: put a known input in inputs/",
	ServiceSampleCheck: "write the sample-check service: fail unless the output of the sample has what that input predicts",
}

// A blank service recipe's service, and the port it provides as a page until
// its author says what the project listens on.
const (
	DraftService = "app"
	DraftPort    = 8080
)

// Placeholder is the command of a service still to be written.
func Placeholder(service string) []string {
	return []string{"sh", "-c", fmt.Sprintf("echo '%s %s' >&2; exit 1", ScaffoldMark, placeholders[service])}
}

// NoLicence is the licence a draft records when GitHub detects none.
const NoLicence = "NOASSERTION: GitHub detected no licence; check the repository before sharing"

// Draft is a new recipe before its author fills it in.
type Draft struct {
	ID          ID
	Repo        string // https://github.com/<Owner>/<Repo>, as GitHub spells it
	Commit      string
	What        string // the task in plain words
	Kind        string
	Licence     string // SPDX identifier; "" when GitHub detects none
	Language    string // the project's main language, which picks a blank recipe's base image
	Compose     []byte // an adopted compose.yaml; nil writes a blank one
	AdaptedFrom string // the adopted file's permanent URL, if it has one
	Source      string // the adopted file, as "docker-compose.yaml in firecrawl/firecrawl"
	Copied      bool   // whether the recipe carries files copied from upstream
	Settings    []Setting
	Access      []Access // what the adopted file reaches for, each why still to write
	Provides    Provides // a service's ports, each what still to write when adopted
	// BasePins holds each base's digest, by its tag, as its registry gave it:
	// a blank recipe's Dockerfile names its bases by digest.
	BasePins map[string]string
}

// Files returns the draft's files by path in the recipe folder: recipe.json
// and compose.yaml, plus a Dockerfile for a blank recipe or a NOTICE for an
// adopted one.
func (d Draft) Files() (map[string][]byte, error) {
	r := Recipe{
		Format:      Format,
		ID:          d.ID.String(),
		Repo:        d.Repo,
		Commit:      d.Commit,
		Task:        d.What,
		Kind:        d.Kind,
		Provides:    d.Provides,
		Packaging:   "authored",
		AdaptedFrom: d.AdaptedFrom,
		Settings:    d.Settings,
		Access:      d.Access,
		Licence:     d.Licence,
	}
	if r.Licence == "" {
		r.Licence = NoLicence
	}
	files := map[string][]byte{}
	if d.Compose != nil {
		r.Packaging = "adapted"
		r.PackagingNote = "Upstream's " + d.Source + ", adopted to the recipe rules by ugt recipe new"
		files[ComposeFile] = d.Compose
		files["NOTICE"] = []byte(d.notice())
	} else {
		r.PackagingNote = "Built from the pinned commit by its own Dockerfile"
		dockerfile, err := d.dockerfile()
		if err != nil {
			return nil, err
		}
		files[ComposeFile] = []byte(d.blankCompose())
		files["Dockerfile"] = []byte(dockerfile)
	}
	record, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	files[RecordFile] = append(record, '\n')
	return files, nil
}

func (d Draft) notice() string {
	licence := d.Licence
	if licence == "" {
		licence = "the licence of its source, which GitHub could not detect"
	}
	text := fmt.Sprintf("compose.yaml is adapted from %s at commit %s", d.Source, d.Commit)
	if d.AdaptedFrom != "" {
		text += " (" + d.AdaptedFrom + ")"
	}
	text += ", and is licensed under " + licence + " like its source."
	if d.Copied {
		text += " Files in upstream/ are copied from the same commit under the same licence."
	}
	text += " The other files in this folder are original to this recipe."
	return strings.Join(Wrap(text, 80, "", ""), "\n") + "\n"
}

// Wrap breaks text into lines of at most width characters, between words,
// starting the first line with first and the others with rest. A word longer
// than a line, such as a URL, gets a line of its own.
func Wrap(text string, width int, first, rest string) []string {
	var lines []string
	line := first
	for _, word := range strings.Fields(text) {
		switch {
		case line == first || line == rest:
			line += word
		case len(line)+1+len(word) > width:
			lines = append(lines, line)
			line = rest + word
		default:
			line += " " + word
		}
	}
	return append(lines, line)
}

func (d Draft) blankCompose() string {
	cmd := func(service string) string {
		quoted := make([]string, 0, 3)
		for _, arg := range Placeholder(service) {
			q, _ := json.Marshal(arg)
			quoted = append(quoted, string(q))
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s/%s: %s. Paths are relative to the workspace root; the\n", d.ID.Owner, d.ID.Repo, d.What)
	b.WriteString("# executor sets UGT_IMAGE and UGT_ARCH and names the project.\nservices:\n")
	if d.Kind == KindService {
		fmt.Fprintf(&b, `  app:    # listens on %d, which recipe.json provides: UGT publishes it
    image: ${UGT_IMAGE}
    build:
      context: ./recipe
    platform: linux/${UGT_ARCH}

`, DraftPort)
	} else {
		fmt.Fprintf(&b, `  task:
    image: ${UGT_IMAGE}
    build:
      context: ./recipe
    platform: linux/${UGT_ARCH}
    volumes:
      - ./inputs:/work/inputs:ro
      - ./outputs:/work/outputs
    command: %s

`, cmd(ServiceTask))
	}
	// a job's check reads outputs offline; a service's check calls the stack
	offline := "    network_mode: none\n"
	if d.Kind == KindService {
		offline = ""
	}
	fmt.Fprintf(&b, `  check:
    image: ${UGT_IMAGE}
    profiles: [check]
%s    volumes:
      - ./outputs:/work/outputs:ro
      - ./recipe/check:/work/check:ro
    command: %s

  sample:
    image: ${UGT_IMAGE}
    profiles: [sample]
    volumes:
      - ./inputs:/work/inputs
      - ./recipe/check:/work/check:ro
    command: %s

  sample-check:
    image: ${UGT_IMAGE}
    profiles: [sample]
    network_mode: none
    volumes:
      - ./outputs:/work/outputs:ro
      - ./recipe/check:/work/check:ro
    command: %s
`, offline, cmd(ServiceCheck), cmd(ServiceSample), cmd(ServiceSampleCheck))
	return b.String()
}

// Bases are the bases a blank recipe's Dockerfile starts from, which its
// author pins in BasePins: the project's language's, then Debian's for a
// build's final stage.
func (d Draft) Bases() []Base {
	switch d.Language {
	case "Python", "Jupyter Notebook":
		return []Base{BasePython}
	case "JavaScript", "TypeScript":
		return []Base{BaseNode}
	case "Go":
		return []Base{BaseGo, BaseDebian}
	case "Rust":
		return []Base{BaseRust, BaseDebian}
	default:
		return []Base{BaseDebian}
	}
}

// dockerfile starts a blank recipe's image from the project's language, each
// base named by digest.
func (d Draft) dockerfile() (string, error) {
	from := map[Base]string{}
	for _, b := range d.Bases() {
		digest := d.BasePins[b.Ref]
		if digest == "" {
			return "", fmt.Errorf("the draft's base %s is not pinned: its digest could not be read from its registry", b.Ref)
		}
		from[b] = "FROM " + b.Ref + "@" + digest
	}
	source := fmt.Sprintf("ADD %s.git#%s /src", d.Repo, d.Commit)
	head := fmt.Sprintf("# %s/%s at its pinned commit, fetched by the build itself, so the host\n# needs no clone. Install it the way its README says; the services in\n# compose.yaml run it from /work.\n", d.ID.Owner, d.ID.Repo)
	var body string
	switch d.Language {
	case "Python", "Jupyter Notebook":
		body = from[BasePython] + "\n" + source + "\nRUN pip install --no-cache-dir /src\n"
	case "JavaScript", "TypeScript":
		body = from[BaseNode] + "\n" + source + "\nWORKDIR /src\nRUN npm ci\n"
	case "Go":
		body = from[BaseGo] + " AS build\n" + source + "\nWORKDIR /src\nRUN go install ./...\n\n" +
			from[BaseDebian] + "\nCOPY --from=build /go/bin/ /usr/local/bin/\n"
	case "Rust":
		body = from[BaseRust] + " AS build\n" + source + "\nRUN cargo install --locked --path /src --root /out\n\n" +
			from[BaseDebian] + "\nCOPY --from=build /out/bin/ /usr/local/bin/\n"
	default:
		body = from[BaseDebian] + "\nRUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \\\n && rm -rf /var/lib/apt/lists/*\n" + source + "\n"
	}
	return head + body + "WORKDIR /work\n", nil
}
