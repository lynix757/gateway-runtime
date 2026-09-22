package storagetarget

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Target struct {
	Provider string `json:"provider"`
	Bucket   string `json:"bucket"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type Registry map[string]Target

func Parse(raw string) (Registry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out Registry
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse storage targets: %w", err)
	}
	for name, target := range out {
		name = strings.TrimSpace(name)
		target.Bucket = strings.TrimSpace(target.Bucket)
		target.Provider = strings.TrimSpace(target.Provider)
		if name == "" || target.Bucket == "" {
			return nil, fmt.Errorf("storage target name and bucket are required")
		}
		if target.Provider == "" {
			target.Provider = "default"
		}
		out[name] = target
	}
	return out, nil
}

func (r Registry) Resolve(name string) (Target, bool) {
	target, ok := r[strings.TrimSpace(name)]
	return target, ok
}
