package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
	f, err := a.recipeFor(ctx, target)
	if err != nil {
		return "", nil, err
	}
	id := f.ID.String()
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
	f, s, err := a.setting(ctx, target, name)
	if err != nil {
		return "", err
	}
	if s.Kind == "secret" {
		if a.Secrets == nil {
			return "", fmt.Errorf("this machine has no secret store UGT can use")
		}
		return f.ID.String(), a.Secrets.Set(ctx, f.ID.String(), name, value)
	}
	return f.ID.String(), a.Values.Set(f.ID.String(), name, value)
}

// ForgetSetting removes a stored setting.
func (a *App) ForgetSetting(ctx context.Context, target, name string) (string, error) {
	f, s, err := a.setting(ctx, target, name)
	if err != nil {
		return "", err
	}
	if s.Kind == "secret" {
		return f.ID.String(), a.Secrets.Delete(ctx, f.ID.String(), name)
	}
	return f.ID.String(), a.Values.Delete(f.ID.String(), name)
}

// SettingKind says whether a recipe's setting is a secret, so a surface knows
// to read it without showing it.
func (a *App) SettingKind(ctx context.Context, target, name string) (recipe.Setting, error) {
	_, s, err := a.setting(ctx, target, name)
	return s, err
}

func (a *App) setting(ctx context.Context, target, name string) (*recipe.Folder, recipe.Setting, error) {
	f, err := a.recipeFor(ctx, target)
	if err != nil {
		return nil, recipe.Setting{}, err
	}
	for _, s := range f.Recipe.Settings {
		if s.Name == name {
			return f, s, nil
		}
	}
	names := f.Recipe.SettingNames()
	if len(names) == 0 {
		return nil, recipe.Setting{}, fmt.Errorf("%s has no settings", f.ID)
	}
	return nil, recipe.Setting{}, fmt.Errorf("%s has no setting %s; it has %s", f.ID, name, strings.Join(names, ", "))
}

// recipeFor resolves owner/repo/task, or owner/repo, to its recipe.
func (a *App) recipeFor(ctx context.Context, target string) (*recipe.Folder, error) {
	parts := strings.Split(strings.Trim(target, "/"), "/")
	task := ""
	if len(parts) == 3 {
		task = parts[2]
		target = parts[0] + "/" + parts[1]
	}
	owner, name, err := recipe.ParseProject(target)
	if err != nil {
		return nil, err
	}
	_, src, err := a.resolve(ctx, owner, name, task)
	if err != nil {
		return nil, err
	}
	return recipe.Load(src)
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
func (a *App) settingsEnv(ctx context.Context, r recipe.Recipe, given map[string]string) (env map[string]string, secrets []string, err error) {
	env = map[string]string{}
	known := map[string]recipe.Setting{}
	for _, s := range r.Settings {
		known[s.Name] = s
		env[s.Name] = "" // set, so Compose does not warn about it
	}
	stored, err := a.values(r.ID)
	if err != nil {
		return nil, nil, err
	}
	for name, v := range stored {
		if _, ok := known[name]; ok {
			env[name] = v
		}
	}
	for name, v := range given {
		s, ok := known[name]
		switch {
		case !ok:
			return nil, nil, fmt.Errorf("%s has no setting %s", r.ID, name)
		case s.Kind == "secret":
			return nil, nil, fmt.Errorf("%s is a secret: it is never passed on a command line or through an agent; set it with ugt settings set %s %s", name, r.ID, name)
		}
		env[name] = v
	}
	for _, s := range r.Settings {
		if s.Kind != "secret" || a.Secrets == nil {
			continue
		}
		secrets = append(secrets, s.Name)
		if v, ok, err := a.Secrets.Get(ctx, r.ID, s.Name); err != nil {
			return nil, nil, err
		} else if ok {
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
			missing = append(missing, fmt.Sprintf("%s, %s.%s Set it with: ugt settings set %s %s", s.Name, s.What, how, r.ID, s.Name))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, nil, fmt.Errorf("%s needs settings the user supplies:\n- %s", r.ID, strings.Join(missing, "\n- "))
	}
	return env, secrets, nil
}
