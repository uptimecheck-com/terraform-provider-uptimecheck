package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// decode reads JSON keeping numbers as written (10000 stays 10000, not 1e+04).
func decode(raw []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// managedConfig is the server's monitor config limited to the keys the user wrote, as compact
// JSON with sorted keys (what jsonencode() produces). The server fills in defaults for every key
// the user leaves out; tracking only the user's keys keeps those from showing up as drift, while
// a changed value for a key the user manages still does.
func managedConfig(user string, server json.RawMessage) (string, error) {
	if user == "" {
		return "", nil
	}
	u, err := decode([]byte(user))
	if err != nil {
		return "", fmt.Errorf("config is not valid JSON: %w", err)
	}
	keys, ok := u.(map[string]any)
	if !ok {
		return "", fmt.Errorf("config must be a JSON object, like jsonencode({ expectedStatus = \"200\" })")
	}
	s := map[string]any{}
	if len(server) > 0 && string(server) != "null" {
		v, err := decode(server)
		if err != nil {
			return "", err
		}
		if m, ok := v.(map[string]any); ok {
			s = m
		}
	}
	out := map[string]any{}
	for k := range keys {
		if v, ok := s[k]; ok {
			out[k] = v
		} else {
			// The server dropped it (unknown key): keep it out, so the plan shows the difference.
			continue
		}
	}
	b, err := json.Marshal(out)
	return string(b), err
}
