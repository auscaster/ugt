package recipe

import (
	"reflect"
	"testing"
)

func TestVariables(t *testing.T) {
	cases := []struct {
		in   string
		want []Variable
	}{
		{"plain ${A} and $B", []Variable{{Name: "A"}, {Name: "B"}}},
		{"$$HOME is literal, $$$C is not", []Variable{{Name: "C"}}},
		{"${PORT:-3000}", []Variable{{Name: "PORT", Default: "3000", HasDefault: true}}},
		{"${PORT-3000}", []Variable{{Name: "PORT", Default: "3000", HasDefault: true}}},
		{"${KEY:?set your key}", []Variable{{Name: "KEY", Required: true}}},
		{"${TLS:+--tls}", []Variable{{Name: "TLS", HasDefault: true}}},
		// a variable inside a default is read too
		{"${A:-${B}}", []Variable{{Name: "A", Default: "${B}", HasDefault: true}, {Name: "B"}}},
		// a use without a default makes the variable needed
		{"${A:-x} ${A}", []Variable{{Name: "A"}}},
		{"${not closed", nil},
		{"${1BAD} $ alone $", nil},
	}
	for _, c := range cases {
		if got := Variables(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Variables(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}
