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
		Packaging:   "authored",
		AdaptedFrom: d.AdaptedFrom,
		Settings:    d.Settings,
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
		files[ComposeFile] = []byte(d.blankCompose())
		files["Dockerfile"] = []byte(d.dockerfile())
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
		fmt.Fprintf(&b, `  app:
    image: ${UGT_IMAGE}
    build:
      context: ./recipe
    platform: linux/${UGT_ARCH}
    ports:
      - "127.0.0.1::8080"    # the port the project listens on

`)
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

// dockerfile starts a blank recipe's image from the project's language.
func (d Draft) dockerfile() string {
	source := fmt.Sprintf("ADD %s.git#%s /src", d.Repo, d.Commit)
	head := fmt.Sprintf("# %s/%s at its pinned commit, fetched by the build itself, so the host\n# needs no clone. Install it the way its README says; the services in\n# compose.yaml run it from /work.\n", d.ID.Owner, d.ID.Repo)
	var body string
	switch d.Language {
	case "Python", "Jupyter Notebook":
		body = "FROM python:3.12-slim-bookworm\n" + source + "\nRUN pip install --no-cache-dir /src\n"
	case "JavaScript", "TypeScript":
		body = "FROM node:22-bookworm-slim\n" + source + "\nWORKDIR /src\nRUN npm ci\n"
	case "Go":
		body = "FROM golang:1.25-bookworm AS build\n" + source + "\nWORKDIR /src\nRUN go install ./...\n\n" +
			"FROM debian:bookworm-slim\nCOPY --from=build /go/bin/ /usr/local/bin/\n"
	case "Rust":
		body = "FROM rust:1-bookworm AS build\n" + source + "\nRUN cargo install --locked --path /src --root /out\n\n" +
			"FROM debian:bookworm-slim\nCOPY --from=build /out/bin/ /usr/local/bin/\n"
	default:
		body = "FROM debian:bookworm-slim\nRUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \\\n && rm -rf /var/lib/apt/lists/*\n" + source + "\n"
	}
	return head + body + "WORKDIR /work\n"
}
