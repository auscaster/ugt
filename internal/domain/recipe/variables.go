package recipe

// Variable is one variable a Compose file reads: ${NAME}, $NAME, or ${NAME}
// with a default (:- or -), a required message (:? or ?) or an alternative
// (:+ or +).
type Variable struct {
	Name       string
	Default    string // the text after :- or -, when HasDefault
	HasDefault bool
	Required   bool // ${NAME:?message}: Compose refuses to run without it
}

// Variables returns every variable text reads, in order of first use, with
// Compose's interpolation rules: $$ is a literal dollar and reads nothing, and
// variables inside a default or message are read too.
func Variables(text string) []Variable {
	var out []Variable
	seen := map[string]int{}
	add := func(v Variable) {
		if i, ok := seen[v.Name]; ok {
			// the strictest use wins: a use with no default makes it needed
			if !v.HasDefault {
				out[i].HasDefault, out[i].Default = false, ""
			}
			out[i].Required = out[i].Required || v.Required
			return
		}
		seen[v.Name] = len(out)
		out = append(out, v)
	}
	scan(text, add)
	return out
}

func scan(s string, add func(Variable)) {
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 >= len(s) {
			continue
		}
		switch next := s[i+1]; {
		case next == '$':
			i++
		case next == '{':
			end, v, inner, ok := braced(s, i+2)
			if !ok {
				continue
			}
			add(v)
			scan(inner, add)
			i = end
		case isNameStart(next):
			j := i + 1
			for j < len(s) && isNameChar(s[j]) {
				j++
			}
			add(Variable{Name: s[i+1 : j]})
			i = j - 1
		}
	}
}

// braced parses NAME[modifier text]} starting at s[start], returning the index
// of the closing brace, the variable and the modifier text.
func braced(s string, start int) (end int, v Variable, inner string, ok bool) {
	j := start
	if j >= len(s) || !isNameStart(s[j]) {
		return 0, v, "", false
	}
	for j < len(s) && isNameChar(s[j]) {
		j++
	}
	v.Name = s[start:j]
	if j < len(s) && s[j] == '}' {
		return j, v, "", true
	}
	if j < len(s) && s[j] == ':' {
		j++
	}
	if j >= len(s) {
		return 0, v, "", false
	}
	modifier := s[j]
	if modifier != '-' && modifier != '?' && modifier != '+' {
		return 0, v, "", false
	}
	j++
	// the text runs to the brace that closes this variable
	depth, k := 1, j
	for ; k < len(s); k++ {
		switch {
		case s[k] == '$' && k+1 < len(s) && s[k+1] == '$':
			k++
		case s[k] == '$' && k+1 < len(s) && s[k+1] == '{':
			depth++
			k++
		case s[k] == '}':
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if depth != 0 {
		return 0, v, "", false
	}
	inner = s[j:k]
	switch modifier {
	case '-':
		v.Default, v.HasDefault = inner, true
	case '?':
		v.Required = true
	case '+':
		// set only when NAME is set: NAME itself stays optional
		v.HasDefault = true
	}
	return k, v, inner, true
}

func isNameStart(c byte) bool { return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
func isNameChar(c byte) bool  { return isNameStart(c) || c >= '0' && c <= '9' }
