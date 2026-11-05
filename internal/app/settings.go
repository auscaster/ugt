package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/auscaster/ugt/internal/domain/catalogue"
	"github.com/auscaster/ugt/internal/domain/ledger"
	"github.com/auscaster/ugt/internal/domain/recipe"
)

// SettingState is one of a recipe's settings and whether it is set. A
// secret's value never leaves the system's secret store.
type SettingState struct {
	recipe.Setting
	Set   bool   `json:"set"`
	Value string `json:"value,omitempty"` // a value setting's value; never a secret's
}

// Settings lists a recipe's settings: target is owner/repo/task, or owner/repo
// when the project has one recipe.
func (a *App) Settings(ctx context.Context, target string) (string, []SettingState, error) {
	f, inst, err := a.recipeFor(ctx, target)
	if err != nil {
		return "", nil, err
	}
	id := inst.String()
	values, err := a.values(id)
	if err != nil {
		return id, nil, err
	}
	var out []SettingState
	for _, s := range f.Recipe.Settings {
		st := SettingState{Setting: s}
		if s.Kind == "secret" {
			if a.Secrets != nil {
				_, st.Set, err = a.Secrets.Get(ctx, id, s.Name)
				if err != nil {
					return id, nil, err
				}
			}
		} else if v, ok := values[s.Name]; ok {
			st.Set, st.Value = true, v
		}
		out = append(out, st)
	}
	return id, out, nil
}

// SetSetting stores one of a recipe's settings: a secret in the system's
// secret store, a value in UGT's state. Surfaces take secrets from the user
// directly, never from an agent.
func (a *App) SetSetting(ctx context.Context, target, name, value string) (string, error) {
	id, s, err := a.setting(ctx, target, name)
	if err != nil {
		return "", err
	}
	if s.Kind == "secret" {
		if a.Secrets == nil {
			return "", fmt.Errorf("this machine has no secret store UGT can use")
		}
		return id, a.storeSecret(ctx, id, name, value)
	}
	return id, a.Values.Set(id, name, value)
}

// ForgetSetting removes a stored setting.
func (a *App) ForgetSetting(ctx context.Context, target, name string) (string, error) {
	id, s, err := a.setting(ctx, target, name)
	if err != nil {
		return "", err
	}
	if s.Kind == "secret" {
		if a.Secrets == nil {
			return "", fmt.Errorf("this machine has no secret store UGT can use")
		}
		if err := a.Secrets.Delete(ctx, id, name); err != nil {
			return "", err
		}
		_ = a.record(ledger.Entry{Kind: ledger.Secret, Name: id + "/" + name, Recipe: id, Removed: a.Now()})
		return id, nil
	}
	return id, a.Values.Delete(id, name)
}

// SettingKind says whether a recipe's setting is a secret, so a surface knows
// to read it without showing it.
func (a *App) SettingKind(ctx context.Context, target, name string) (recipe.Setting, error) {
	_, s, err := a.setting(ctx, target, name)
	return s, err
}

func (a *App) setting(ctx context.Context, target, name string) (string, recipe.Setting, error) {
	f, inst, err := a.recipeFor(ctx, target)
	if err != nil {
		return "", recipe.Setting{}, err
	}
	for _, s := range f.Recipe.Settings {
		if s.Name == name {
			return inst.String(), s, nil
		}
	}
	names := f.Recipe.SettingNames()
	if len(names) == 0 {
		return "", recipe.Setting{}, fmt.Errorf("%s has no settings", f.ID)
	}
	return "", recipe.Setting{}, fmt.Errorf("%s has no setting %s; it has %s", f.ID, name, strings.Join(names, ", "))
}

// recipeFor resolves owner/repo/task, or owner/repo, to its recipe.
func (a *App) recipeFor(ctx context.Context, target string) (*recipe.Folder, recipe.Installation, error) {
	base := target
	var inst recipe.Installation
	if strings.Contains(target, "@") {
		var err error
		inst, err = recipe.ParseInstallation(target)
		if err != nil {
			return nil, inst, err
		}
		base = inst.ID.String()
	}
	_, src, err := a.resolveTarget(ctx, base)
	if err != nil {
		return nil, inst, err
	}
	f, err := recipe.Load(src)
	if err != nil {
		return nil, inst, err
	}
	inst.ID = f.ID
	if inst.Name != "" {
		if err := f.Recipe.NamedService(); err != nil {
			return nil, inst, err
		}
	}
	return f, inst, nil
}

// resolveTarget resolves owner/repo/task, or owner/repo when the project has
// one recipe, to the recipe's catalogue entry and folder.
func (a *App) resolveTarget(ctx context.Context, target string) (catalogue.Entry, fs.FS, error) {
	parts := strings.Split(strings.Trim(target, "/"), "/")
	task := ""
	if len(parts) == 3 {
		task = parts[2]
		target = parts[0] + "/" + parts[1]
	}
	owner, name, err := recipe.ParseProject(target)
	if err != nil {
		return catalogue.Entry{}, nil, err
	}
	return a.resolve(ctx, owner, name, task)
}

func (a *App) values(id string) (map[string]string, error) {
	if a.Values == nil {
		return map[string]string{}, nil
	}
	return a.Values.Get(id)
}

// settingsEnv gathers a recipe's settings for a run: stored values, then the
// values given for this run, then secrets from the system's secret store. A
// required setting that is missing stops the run with the command that sets
// it. Secrets are never accepted as given values.
func (a *App) settingsEnv(ctx context.Context, r recipe.Recipe, id string, given map[string]string) (env map[string]string, secrets []string, err error) {
	known := map[string]recipe.Setting{}
	for _, s := range r.Settings {
		known[s.Name] = s
	}
	if env, err = a.valuesEnv(r, id); err != nil {
		return nil, nil, err
	}
	for name, v := range given {
		s, ok := known[name]
		switch {
		case !ok:
			return nil, nil, fmt.Errorf("%s has no setting %s", id, name)
		case s.Kind == "secret":
			return nil, nil, fmt.Errorf("%s is a secret: it is never passed on a command line or through an agent; set it with ugt settings set %s %s", name, id, name)
		}
		env[name] = v
	}
	for _, s := range r.Settings {
		if s.Kind != "secret" || a.Secrets == nil {
			continue
		}
		secrets = append(secrets, s.Name)
		if v, ok, err := a.Secrets.Get(ctx, id, s.Name); err != nil {
			return nil, nil, err
		} else if ok {
			env[s.Name] = v
		}
	}
	for _, s := range r.Settings {
		if s.Kind == "secret" && s.Required && env[s.Name] == "" {
			v, err := a.askSecret(ctx, id, s)
			if err != nil {
				return nil, nil, err
			}
			env[s.Name] = v
		}
	}
	var missing []string
	for _, s := range r.Settings {
		if s.Required && env[s.Name] == "" {
			how := ""
			if s.How != "" {
				how = " " + s.How + "."
			}
			missing = append(missing, fmt.Sprintf("%s, %s.%s Set it with: ugt settings set %s %s", s.Name, s.What, how, id, s.Name))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, nil, fmt.Errorf("%s needs settings the user supplies:\n- %s", id, strings.Join(missing, "\n- "))
	}
	return env, secrets, nil
}

// valuesEnv is a recipe's settings with their stored values: every declared
// setting is set, empty when it has no value, so Compose does not warn.
func (a *App) valuesEnv(r recipe.Recipe, id string) (map[string]string, error) {
	env := map[string]string{}
	for _, s := range r.Settings {
		env[s.Name] = ""
	}
	stored, err := a.values(id)
	if err != nil {
		return nil, err
	}
	for name, v := range stored {
		if _, ok := env[name]; ok {
			env[name] = v
		}
	}
	return env, nil
}

// askSecret asks the user for a missing secret through UGT's own prompt,
// never an agent, and keeps it in the system's secret store. It returns ""
// when nothing can ask the user here, or they entered nothing.
func (a *App) askSecret(ctx context.Context, id string, s recipe.Setting) (string, error) {
	if a.Prompt == nil || a.Secrets == nil {
		return "", nil
	}
	msg := fmt.Sprintf("%s needs %s: %s.", id, s.Name, s.What)
	if s.How != "" {
		msg += " " + s.How + "."
	}
	msg += "\n\nUGT keeps it in this computer's secret store. Your AI app never sees it."
	v, err := a.Prompt.Secret(ctx, Question{Title: "UGT", Message: msg})
	if errors.Is(err, ErrNoPrompt) || v == "" {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if err := a.storeSecret(ctx, id, s.Name, v); err != nil {
		return "", err
	}
	return v, nil
}

// storeSecret keeps a secret, and records it in the ledger, so uninstall
// finds it without asking any catalogue.
func (a *App) storeSecret(ctx context.Context, id, name, value string) error {
	if err := a.Secrets.Set(ctx, id, name, value); err != nil {
		return err
	}
	_ = a.record(ledger.Entry{Kind: ledger.Secret, Name: id + "/" + name, Recipe: id})
	return nil
}
