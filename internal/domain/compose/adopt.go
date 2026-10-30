package compose

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/auscaster/ugt/internal/domain/recipe"
)

// Source is where an adopted Compose file came from.
type Source struct {
	Repo   string // owner/repo that its build contexts and mounted files belong to; "" when unknown
	Commit string // the full commit they are pinned to
	Dir    string // the file's folder in that repository, slash-separated; "" for the root
	Name   string // how the recipe names it, as "docker-compose.yaml in firecrawl/firecrawl"
}

// Adoption is an existing Compose file rewritten to the recipe rules.
type Adoption struct {
	Compose  []byte
	Kind     string           // service when anything publishes a port, otherwise job
	Settings []recipe.Setting // the host variables it reads, now declared
	Fetch    []Fetch          // files the original read from its repository, which the recipe carries
	Builds   []string         // services that build from source at the pinned commit
	Changes  []string         // what was rewritten
	Problems []string         // what could not be fixed safely; the recipe fails until each is resolved
}

// Fetch is one file or folder to copy from the source repository into the
// recipe folder.
type Fetch struct {
	From string // path in the source repository
	To   string // path in the recipe folder
}

// UpstreamDir is where an adopted recipe keeps the files it copies from its
// source repository.
const UpstreamDir = "upstream"

// ProbeImage is the small public image that runs adopted recipes' probes and
// placeholder services.
const ProbeImage = "busybox:1.37"

// Images returns the published images an adoption needs pinned, with any
// variable defaults filled in: every image not built by the file itself, and
// the probe image. Images already pinned by digest are left out.
func Images(data []byte) ([]string, error) {
	root, err := parse(data)
	if err != nil {
		return nil, err
	}
	built := map[string]bool{}
	services := lookup(root, "services")
	eachService(services, func(name string, svc *yaml.Node) {
		if lookup(svc, "build") != nil {
			if image := lookup(svc, "image"); image != nil {
				built[image.Value] = true
			}
		}
	})
	set := map[string]bool{ProbeImage: true}
	eachService(services, func(name string, svc *yaml.Node) {
		image := lookup(svc, "image")
		if image == nil || lookup(svc, "build") != nil || built[image.Value] {
			return
		}
		ref := fillDefaults(image.Value)
		if !strings.Contains(ref, "@sha256:") && !strings.Contains(ref, "$") {
			set[ref] = true
		}
	})
	return sortedSet(set), nil
}

// Adopt rewrites an existing Compose file to the recipe rules. pins maps each
// image from Images to its digest ("sha256:..."); an image without a pin is a
// problem. kind is job or service, or "" to infer it: a file that publishes a
// port is a service. The original's comments, order and anchors are kept.
func Adopt(data []byte, src Source, pins map[string]string, kind string) (Adoption, error) {
	doc, err := parseDocument(data)
	if err != nil {
		return Adoption{}, err
	}
	root := doc.Content[0]
	a := &adopter{src: src, pins: pins, root: root, volumes: map[string]bool{}, pinned: map[string]bool{}}
	if remove(root, "name") {
		a.change("removed the top-level name: the executor names the project")
	}
	if remove(root, "version") {
		a.change("removed the obsolete top-level version")
	}
	services := lookup(root, "services")
	if services == nil || services.Kind != yaml.MappingNode || len(services.Content) == 0 {
		return Adoption{}, fmt.Errorf("the Compose file has no services")
	}
	if top := lookup(root, "volumes"); top != nil {
		for i := 0; i+1 < len(top.Content); i += 2 {
			a.volumes[top.Content[i].Value] = true
			if lookup(top.Content[i+1], "external") != nil {
				a.problem("volume %s is external: it must exist before the recipe runs; make it a plain named volume", top.Content[i].Value)
			}
		}
	}

	a.images(services)
	eachService(services, a.service)

	if kind == "" {
		kind = recipe.KindJob
		eachService(services, func(_ string, svc *yaml.Node) {
			if lookup(svc, "ports") != nil {
				kind = recipe.KindService
			}
		})
	}
	a.conventional(services, kind)
	a.settings(root)

	first := "Adapted by ugt recipe new from " + src.Name
	if src.Commit != "" {
		first += ", at commit " + src.Commit
	}
	header := strings.Join(recipe.Wrap(first+"; see NOTICE. Changes:", 78, "", ""), "\n")
	for _, c := range a.changes {
		header += "\n" + strings.Join(recipe.Wrap(c, 76, "- ", "  "), "\n")
	}
	doc.HeadComment = strings.TrimSpace(header + "\n\n" + doc.HeadComment)

	plainMergeKeys(doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return Adoption{}, err
	}
	if err := enc.Close(); err != nil {
		return Adoption{}, err
	}
	return Adoption{
		Compose:  buf.Bytes(),
		Kind:     kind,
		Settings: a.declared,
		Fetch:    a.fetch,
		Builds:   a.builds,
		Changes:  a.changes,
		Problems: a.problems,
	}, nil
}

type adopter struct {
	src      Source
	pins     map[string]string
	root     *yaml.Node
	volumes  map[string]bool // named volumes declared at the top level
	renamed  map[string]string
	pinned   map[string]bool
	declared []recipe.Setting
	builds   []string
	fetch    []Fetch
	changes  []string
	problems []string
}

func (a *adopter) change(format string, args ...any) {
	a.changes = append(a.changes, fmt.Sprintf(format, args...))
}

func (a *adopter) problem(format string, args ...any) {
	a.problems = append(a.problems, fmt.Sprintf(format, args...))
}

// images names what each built service builds, and pins every other image.
func (a *adopter) images(services *yaml.Node) {
	var builders []string
	eachService(services, func(name string, svc *yaml.Node) {
		if lookup(svc, "build") != nil {
			builders = append(builders, name)
		}
	})
	a.renamed = map[string]string{}
	for _, name := range builders {
		image := "${UGT_IMAGE}"
		if len(builders) > 1 {
			image += "-" + tagSafe(name)
		}
		svc := lookup(services, name)
		if old := lookup(svc, "image"); old != nil {
			a.renamed[old.Value] = image
		}
		set(svc, "image", str(image))
		a.build(name, svc)
		if lookup(svc, "platform") == nil {
			set(svc, "platform", str("linux/${UGT_ARCH}"))
		}
	}
	eachService(services, func(name string, svc *yaml.Node) {
		image := lookup(svc, "image")
		if image == nil || lookup(svc, "build") != nil {
			return
		}
		if built, ok := a.renamed[image.Value]; ok {
			image.Value, image.Style = built, 0
			return
		}
		ref := fillDefaults(image.Value)
		switch {
		case strings.Contains(ref, "$"):
			a.problem("services.%s.image %q reads a variable with no default: name the image", name, image.Value)
		case strings.Contains(ref, "@sha256:"):
			image.Value = ref
		case a.pins[ref] != "":
			image.Value, image.Style = ref+"@"+a.pins[ref], 0
			if !a.pinned[ref] {
				a.pinned[ref] = true
				a.change("pinned %s by digest", ref)
			}
		default:
			a.problem("services.%s.image %s could not be pinned by digest", name, ref)
		}
	})
}

// build turns a service's build context into a Git context at the commit,
// where the build is defined: in an anchor shared by several services, it is
// rewritten once for all of them.
func (a *adopter) build(name string, svc *yaml.Node) {
	defining := owner(svc, "build")
	build := lookup(defining, "build")
	context := "."
	if build.Kind == yaml.ScalarNode {
		context = build.Value
		m := &yaml.Node{Kind: yaml.MappingNode}
		set(defining, "build", m)
		build = m
	} else if c := lookup(build, "context"); c != nil {
		context = c.Value
	}
	at := "services." + name + ".build"
	if strings.Contains(context, "://") || strings.HasPrefix(context, "git@") {
		if !pinnedRef.MatchString(context) {
			a.problem("%s context %s is not pinned to a commit: add #<full commit SHA>", at, context)
		}
		return
	}
	if a.src.Repo == "" {
		a.problem("%s builds from %s on this machine; adopt from the repository so the build can fetch it at a pinned commit", at, context)
		return
	}
	dir := path.Clean(path.Join(a.src.Dir, context))
	if strings.HasPrefix(dir, "..") {
		a.problem("%s context %s is outside the repository", at, context)
		return
	}
	git := fmt.Sprintf("https://github.com/%s.git#%s", a.src.Repo, a.src.Commit)
	if dir != "." {
		git += ":" + dir
	}
	set(build, "context", str(git))
	a.builds = append(a.builds, name)
	a.change("services.%s builds from the repository at the pinned commit", name)
	if extra := lookup(build, "additional_contexts"); extra != nil {
		a.problem("%s.additional_contexts reads folders on this machine: fetch them in the Dockerfile instead", at)
	}
}

func (a *adopter) service(name string, svc *yaml.Node) {
	at := "services." + name
	if remove(svc, "container_name") {
		a.change("%s: removed container_name; names come from the executor's project", at)
	}
	if p := lookup(svc, "privileged"); p != nil && p.Value == "true" {
		a.problem("%s is privileged: recipes get no extra host access", at)
	}
	for _, key := range []string{"network_mode", "pid", "ipc", "userns_mode"} {
		if v := lookup(svc, key); v != nil && v.Value == "host" {
			a.problem("%s uses the host's %s: use the project's own network", at, strings.TrimSuffix(key, "_mode"))
		}
	}
	for _, key := range []string{"cap_add", "devices"} {
		if lookup(svc, key) != nil {
			a.problem("%s sets %s: recipes get no extra host access", at, key)
		}
	}
	if lookup(svc, "extends") != nil {
		a.problem("%s extends another file: copy what it needs into this service", at)
	}
	if hosts := lookup(owner(svc, "extra_hosts"), "extra_hosts"); hosts != nil && hostGateway(hosts) {
		remove(owner(svc, "extra_hosts"), "extra_hosts")
		a.change("%s: dropped extra_hosts pointing at host-gateway; recipes do not reach the host machine", at)
	}
	if ports := lookup(svc, "ports"); ports != nil {
		for _, p := range ports.Content {
			a.port(at, p)
		}
	}
	if volumes := lookup(svc, "volumes"); volumes != nil {
		kept := volumes.Content[:0]
		for _, v := range volumes.Content {
			if a.volume(at, v) {
				kept = append(kept, v)
			}
		}
		volumes.Content = kept
	}
	if files := lookup(svc, "env_file"); files != nil {
		a.envFiles(at, files)
	}
}

// port publishes a port on 127.0.0.1 only, on a free host port the engine
// chooses, so a recipe never collides with what already runs on the machine.
func (a *adopter) port(at string, p *yaml.Node) {
	if p.Kind == yaml.MappingNode {
		set(p, "host_ip", str("127.0.0.1"))
		remove(p, "published")
		a.change("%s publishes %s on a free 127.0.0.1 port", at, valueOf(lookup(p, "target")))
		return
	}
	spec := fillDefaults(p.Value)
	proto := ""
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		spec, proto = spec[:i], spec[i:]
	}
	if strings.HasPrefix(spec, "[") { // [::]:8080:80
		if end := strings.Index(spec, "]:"); end >= 0 {
			spec = spec[end+2:]
		}
	}
	parts := strings.Split(spec, ":")
	container := parts[len(parts)-1]
	local := "127.0.0.1::" + container + proto
	if local != p.Value {
		p.Value, p.Tag, p.Style = local, "!!str", yaml.DoubleQuotedStyle
		a.change("%s publishes %s on a free 127.0.0.1 port", at, container+proto)
	}
}

// volume rewrites one mount and reports whether to keep it.
func (a *adopter) volume(at string, v *yaml.Node) bool {
	var source, target, mode string
	long := v.Kind == yaml.MappingNode
	if long {
		if t := lookup(v, "type"); t == nil || t.Value != "bind" {
			return true // a named volume, tmpfs or anonymous volume
		}
		source, target = valueOf(lookup(v, "source")), valueOf(lookup(v, "target"))
		if ro := lookup(v, "read_only"); ro != nil && ro.Value == "true" {
			mode = "ro"
		}
	} else {
		parts := strings.SplitN(v.Value, ":", 3)
		if len(parts) == 1 {
			return true // an anonymous volume
		}
		source, target = parts[0], parts[1]
		if len(parts) == 3 {
			mode = parts[2]
		}
	}
	if !isHostPath(source) {
		return true // a named volume
	}
	readOnly := false
	for _, opt := range strings.Split(mode, ",") {
		readOnly = readOnly || opt == "ro"
	}
	setSource := func(s string) {
		if long {
			set(v, "source", str(s))
			return
		}
		v.Value = s + ":" + target
		if mode != "" {
			v.Value += ":" + mode
		}
		v.Tag, v.Style = "!!str", 0
	}
	switch {
	case strings.Contains(source, "docker.sock"):
		a.problem("%s mounts the container engine's socket (%s): recipes never get it", at, source)
		return true
	case source == "/etc/localtime" || source == "/etc/timezone":
		a.change("%s: dropped the %s mount; containers keep UTC", at, source)
		return false
	case strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || source == "." || source == "..":
		if clean := path.Clean(source); clean == "inputs" || clean == "outputs" ||
			strings.HasPrefix(clean, "inputs/") || strings.HasPrefix(clean, "outputs/") {
			return true
		}
		from := path.Clean(path.Join(a.src.Dir, source))
		if strings.HasPrefix(from, "..") {
			a.problem("%s mounts %s, outside the repository", at, source)
			return true
		}
		if readOnly || looksLikeFile(source) {
			to := path.Join(UpstreamDir, from)
			a.fetch = append(a.fetch, Fetch{From: from, To: to})
			setSource("./recipe/" + to)
			a.change("%s mounts %s from the recipe's own copy, %s", at, source, to)
			return true
		}
	case readOnly:
		a.problem("%s mounts %s from this machine read-only: copy what it needs into the recipe and mount it from ./recipe", at, source)
		return true
	}
	name := a.volumeName(source)
	setSource(name)
	a.change("%s keeps %s in the named volume %s", at, source, name)
	return true
}

// volumeName declares a named volume for data the original kept on the host.
func (a *adopter) volumeName(source string) string {
	base := strings.ToLower(path.Base(strings.TrimRight(source, "/")))
	base = strings.Trim(unsafeName.ReplaceAllString(base, "-"), "-.")
	if base == "" {
		base = "data"
	}
	name := base
	for i := 2; a.volumes[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	a.volumes[name] = true
	top := lookup(a.root, "volumes")
	if top == nil {
		top = &yaml.Node{Kind: yaml.MappingNode}
		set(a.root, "volumes", top)
	}
	set(top, name, &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle})
	return name
}

// envFiles points env_file entries at the recipe's copies of them.
func (a *adopter) envFiles(at string, files *yaml.Node) {
	entries := []*yaml.Node{files}
	if files.Kind == yaml.SequenceNode {
		entries = files.Content
	}
	for _, e := range entries {
		node := e
		if e.Kind == yaml.MappingNode {
			node = lookup(e, "path")
		}
		if node == nil || node.Kind != yaml.ScalarNode {
			continue
		}
		from := path.Clean(path.Join(a.src.Dir, node.Value))
		if path.IsAbs(node.Value) || strings.HasPrefix(from, "..") {
			a.problem("%s reads %s from this machine: move the variables it sets into environment", at, node.Value)
			continue
		}
		to := path.Join(UpstreamDir, from)
		a.fetch = append(a.fetch, Fetch{From: from, To: to})
		node.Value = "./recipe/" + to // relative paths resolve from the workspace
		a.change("%s reads its env_file from the recipe's own copy, %s", at, to)
	}
}

// conventional adds the services every recipe names, as placeholders for the
// author, and a starting HTTP probe as a service recipe's check.
func (a *adopter) conventional(services *yaml.Node, kind string) {
	probe := ProbeImage
	if digest := a.pins[ProbeImage]; digest != "" {
		probe += "@" + digest
	} else {
		a.problem("%s could not be pinned by digest", ProbeImage)
	}
	placeholder := func(name, profile string) *yaml.Node {
		svc := &yaml.Node{Kind: yaml.MappingNode}
		set(svc, "image", str(probe))
		if profile != "" {
			set(svc, "profiles", flow(profile))
		}
		set(svc, "network_mode", str("none"))
		command := flow(recipe.Placeholder(name)...)
		command.Content[len(command.Content)-1].Style = yaml.DoubleQuotedStyle
		set(svc, "command", command)
		return svc
	}
	add := func(name, comment string, svc *yaml.Node) {
		if lookup(services, name) != nil {
			a.problem("services.%s already exists in the original; rename it, because UGT runs %s by that name", name, name)
			return
		}
		key := str(name)
		key.HeadComment = comment
		services.Content = append(services.Content, key, svc)
	}
	const write = "Added by ugt recipe new: write it for this recipe."
	if kind == recipe.KindJob {
		add(recipe.ServiceTask, write, placeholder(recipe.ServiceTask, ""))
	}
	if target := a.probeTarget(services); kind == recipe.KindService && target != "" {
		check := &yaml.Node{Kind: yaml.MappingNode}
		set(check, "image", str(probe))
		set(check, "profiles", flow(recipe.ProfileCheck))
		set(check, "command", flow("wget", "-q", "-O", "/dev/null", target))
		add(recipe.ServiceCheck, "Added by ugt recipe new: a starting check that fails unless the stack answers\nHTTP. Make it fail unless the service really works.", check)
	} else {
		add(recipe.ServiceCheck, write, placeholder(recipe.ServiceCheck, recipe.ProfileCheck))
	}
	add(recipe.ServiceSample, write, placeholder(recipe.ServiceSample, recipe.ProfileSample))
	add(recipe.ServiceSampleCheck, write, placeholder(recipe.ServiceSampleCheck, recipe.ProfileSample))
}

// probeTarget is the URL of the first published port, from inside the stack.
func (a *adopter) probeTarget(services *yaml.Node) string {
	var target string
	eachService(services, func(name string, svc *yaml.Node) {
		ports := lookup(svc, "ports")
		if target != "" || ports == nil || len(ports.Content) == 0 {
			return
		}
		p := ports.Content[0]
		container := valueOf(lookup(p, "target"))
		if p.Kind == yaml.ScalarNode {
			spec := p.Value
			if i := strings.LastIndex(spec, "/"); i >= 0 {
				spec = spec[:i]
			}
			container = spec[strings.LastIndex(spec, ":")+1:]
		}
		if first, _, _ := strings.Cut(container, "-"); first != "" {
			target = fmt.Sprintf("http://%s:%s/", name, first)
		}
	})
	return target
}

var (
	secretName  = regexp.MustCompile(`KEY|TOKEN|SECRET|PASS|PWD|CREDENTIAL|AUTH|PRIVATE|SALT|DSN|COOKIE|SESSION`)
	settingName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	pinnedRef   = regexp.MustCompile(`#[0-9a-f]{40}`)
	unsafeName  = regexp.MustCompile(`[^a-z0-9_.-]+`)
	withDefault = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:?-([^}$]*)\}`)
	// a variable with a plain, non-empty default: ${NAME:-value}
	fixedDefault = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*):?-([^}$]+)\}`)
)

// settings fixes each variable that has a default at that default, and
// declares the rest, which the user may supply, as settings. As in Compose,
// only ${NAME:?message} is required; ${NAME} alone is empty when unset.
func (a *adopter) settings(root *yaml.Node) {
	fixed := map[string]bool{}
	walkScalars(root, func(n *yaml.Node) {
		for {
			m := fixedDefault.FindStringSubmatch(n.Value)
			if m == nil {
				return
			}
			fixed[m[1]] = true
			n.Value = strings.Replace(n.Value, m[0], m[2], 1)
		}
	})
	if len(fixed) > 0 {
		a.change("fixed %d variables at their defaults: %s", len(fixed), strings.Join(sortedSet(fixed), ", "))
	}
	var b strings.Builder
	walkScalars(root, func(n *yaml.Node) { b.WriteString(n.Value); b.WriteByte(0) })
	for _, v := range recipe.Variables(b.String()) {
		switch {
		case v.Name == recipe.VarImage || v.Name == recipe.VarArch:
			continue
		case !settingName.MatchString(v.Name) || hasAnyPrefix(v.Name, "UGT_", "COMPOSE_", "DOCKER_"):
			a.problem("${%s} cannot be a setting: settings are upper-case and never start with UGT_, COMPOSE_ or DOCKER_; rename it or set a value", v.Name)
			continue
		}
		kind := "value"
		if secretName.MatchString(v.Name) {
			kind = "secret"
		}
		what := "Read by the original Compose file as ${" + v.Name + "}"
		if !v.Required {
			what += "; empty when not set"
		}
		a.declared = append(a.declared, recipe.Setting{Name: v.Name, Kind: kind, Required: v.Required, What: what})
	}
	if len(a.declared) > 0 {
		a.change("declared %d host variables as settings", len(a.declared))
	}
}

// hostGateway reports whether extra_hosts maps any name to the host machine.
func hostGateway(hosts *yaml.Node) bool {
	found := false
	walkScalars(hosts, func(n *yaml.Node) { found = found || strings.Contains(n.Value, "host-gateway") })
	if hosts.Kind == yaml.MappingNode {
		for i := 1; i < len(hosts.Content); i += 2 {
			found = found || strings.Contains(hosts.Content[i].Value, "host-gateway")
		}
	}
	return found
}

// fillDefaults replaces each ${NAME:-default} with its default.
func fillDefaults(s string) string {
	return withDefault.ReplaceAllString(s, "$1")
}

func isHostPath(s string) bool {
	return strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, "$")
}

// looksLikeFile reports whether a mount source names a file, by its extension
// or a leading dot.
func looksLikeFile(source string) bool {
	base := path.Base(source)
	return base != "." && base != ".." && strings.Contains(base, ".")
}

func tagSafe(s string) string {
	return strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(s), "-"), "-.")
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// YAML node helpers. Lookups follow aliases and merge keys (<<), so values
// shared through anchors are read, and rewritten, where they are defined.

func parseDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("the Compose file is not valid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the Compose file is not a YAML mapping")
	}
	return &doc, nil
}

func parse(data []byte) (*yaml.Node, error) {
	doc, err := parseDocument(data)
	if err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func lookup(m *yaml.Node, key string) *yaml.Node {
	m = deref(m)
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i].Tag != "!!merge" {
			return deref(m.Content[i+1])
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Tag == "!!merge" || m.Content[i].Value == "<<" {
			sources := []*yaml.Node{deref(m.Content[i+1])}
			if sources[0].Kind == yaml.SequenceNode {
				sources = sources[0].Content
			}
			for _, s := range sources {
				if v := lookup(s, key); v != nil {
					return v
				}
			}
		}
	}
	return nil
}

// owner returns the mapping that defines key for m: m itself, or the anchor
// it merges the key from.
func owner(m *yaml.Node, key string) *yaml.Node {
	m = deref(m)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i].Tag != "!!merge" {
			return m
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Tag == "!!merge" || m.Content[i].Value == "<<" {
			sources := []*yaml.Node{deref(m.Content[i+1])}
			if sources[0].Kind == yaml.SequenceNode {
				sources = sources[0].Content
			}
			for _, s := range sources {
				if lookup(s, key) != nil {
					return owner(s, key)
				}
			}
		}
	}
	return m
}

func set(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i].Tag != "!!merge" {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, str(key), value)
}

func remove(m *yaml.Node, key string) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i].Tag != "!!merge" {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

func eachService(services *yaml.Node, fn func(name string, svc *yaml.Node)) {
	services = deref(services)
	if services == nil || services.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(services.Content); i += 2 {
		if svc := deref(services.Content[i+1]); svc != nil && svc.Kind == yaml.MappingNode {
			fn(services.Content[i].Value, svc)
		}
	}
}

// plainMergeKeys clears the tag yaml.v3 decodes merge keys with, which it
// would otherwise write back as "!!merge <<".
func plainMergeKeys(n *yaml.Node) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Tag == "!!merge" {
				n.Content[i].Tag = ""
			}
		}
	}
	for _, c := range n.Content {
		plainMergeKeys(c)
	}
}

func walkScalars(n *yaml.Node, fn func(*yaml.Node)) {
	switch n.Kind {
	case yaml.ScalarNode:
		fn(n)
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			walkScalars(n.Content[i], fn)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, c := range n.Content {
			walkScalars(c, fn)
		}
	}
}

func valueOf(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

func str(s string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }

func flow(items ...string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, it := range items {
		seq.Content = append(seq.Content, str(it))
	}
	return seq
}
