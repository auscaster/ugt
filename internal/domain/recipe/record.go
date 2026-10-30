package recipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// SetFields returns recipe.json with the given top-level fields set, keeping
// every other field, extension fields included, in its place and order. A
// field not there yet is added at the end.
func SetFields(record []byte, fields map[string]any) ([]byte, error) {
	type field struct {
		key   string
		value json.RawMessage
	}
	dec := json.NewDecoder(bytes.NewReader(record))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, fmt.Errorf("%s is not a JSON object", RecordFile)
	}
	var order []field
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		order = append(order, field{t.(string), v})
	}
	set := func(key string, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		for i := range order {
			if order[i].key == key {
				order[i].value = data
				return nil
			}
		}
		order = append(order, field{key, data})
		return nil
	}
	for _, key := range sortedFieldKeys(fields) {
		if err := set(key, fields[key]); err != nil {
			return nil, err
		}
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, f := range order {
		key, _ := json.Marshal(f.key)
		var value bytes.Buffer
		if err := json.Indent(&value, f.value, "  ", "  "); err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "  %s: %s", key, value.Bytes())
		if i < len(order)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes(), nil
}

func sortedFieldKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
