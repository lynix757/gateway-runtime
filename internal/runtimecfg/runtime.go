package runtimecfg

import (
	"fmt"
	"sort"
	"strings"
)

type Role string

const (
	RoleREBFF     Role = "rebff"
	RoleStorage   Role = "storage"
	RoleStreaming Role = "streaming"
	RoleWebSocket Role = "websocket"
)

type Selection struct {
	Roles        []Role
	Capabilities map[Role][]string
}

var allowed = map[Role]map[string]struct{}{
	RoleREBFF:     set("oidc", "session", "token-refresh", "csrf", "user-context", "api-proxy", "coarse-authz", "audit", "trace-propagation"),
	RoleStorage:   set("upload", "download", "multipart", "presign", "metadata", "audit"),
	RoleStreaming: set("playback", "manifest", "token", "origin-select", "audit"),
	RoleWebSocket: set("connect", "publish", "subscribe", "presence", "heartbeat", "audit"),
}

var defaults = map[Role][]string{
	RoleREBFF:     {"oidc", "session", "token-refresh", "csrf", "user-context", "api-proxy", "coarse-authz", "audit", "trace-propagation"},
	RoleStorage:   {"upload", "download", "multipart", "presign", "metadata", "audit"},
	RoleStreaming: {"playback", "manifest", "token", "origin-select", "audit"},
	RoleWebSocket: {"connect", "publish", "subscribe", "presence", "heartbeat", "audit"},
}

func set(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

func ParseRoles(raw string) ([]Role, error) {
	if strings.TrimSpace(raw) == "" {
		return []Role{RoleREBFF}, nil
	}
	seen := map[Role]bool{}
	var out []Role
	for _, part := range strings.Split(raw, ",") {
		role := Role(strings.ToLower(strings.TrimSpace(part)))
		if _, ok := allowed[role]; !ok {
			return nil, fmt.Errorf("unsupported runtime role %q", role)
		}
		if !seen[role] {
			seen[role] = true
			out = append(out, role)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one runtime role is required")
	}
	return out, nil
}

func ParseCapabilities(roles []Role, raw string) (map[Role][]string, error) {
	out := make(map[Role][]string, len(roles))
	if strings.TrimSpace(raw) == "" {
		for _, role := range roles {
			out[role] = append([]string(nil), defaults[role]...)
		}
		return out, nil
	}
	if len(roles) == 1 && !strings.Contains(raw, ":") {
		caps, err := parseCapList(roles[0], strings.NewReplacer("|", ",").Replace(raw))
		if err != nil {
			return nil, err
		}
		out[roles[0]] = caps
		return out, nil
	}
	selected := map[Role]bool{}
	for _, r := range roles {
		selected[r] = true
	}
	for _, group := range strings.Split(raw, ";") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		parts := strings.SplitN(group, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("multi-role capabilities must use role:capability|capability syntax")
		}
		role := Role(strings.ToLower(strings.TrimSpace(parts[0])))
		if !selected[role] {
			return nil, fmt.Errorf("capabilities configured for unselected role %q", role)
		}
		caps, err := parseCapList(role, strings.ReplaceAll(parts[1], "|", ","))
		if err != nil {
			return nil, err
		}
		out[role] = caps
	}
	for _, role := range roles {
		if len(out[role]) == 0 {
			out[role] = append([]string(nil), defaults[role]...)
		}
	}
	return out, nil
}

func parseCapList(role Role, raw string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		cap := strings.ToLower(strings.TrimSpace(part))
		if cap == "" {
			continue
		}
		if _, ok := allowed[role][cap]; !ok {
			return nil, fmt.Errorf("unsupported capability %q for role %q", cap, role)
		}
		if !seen[cap] {
			seen[cap] = true
			out = append(out, cap)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("role %q requires at least one capability", role)
	}
	return out, nil
}

func Validate(sel Selection) error {
	if len(sel.Roles) == 0 {
		return fmt.Errorf("at least one runtime role is required")
	}
	seen := map[Role]bool{}
	for _, role := range sel.Roles {
		if _, ok := allowed[role]; !ok {
			return fmt.Errorf("unsupported runtime role %q", role)
		}
		if seen[role] {
			return fmt.Errorf("duplicate runtime role %q", role)
		}
		seen[role] = true
		if len(sel.Capabilities[role]) == 0 {
			return fmt.Errorf("role %q requires at least one capability", role)
		}
		for _, cap := range sel.Capabilities[role] {
			if _, ok := allowed[role][cap]; !ok {
				return fmt.Errorf("unsupported capability %q for role %q", cap, role)
			}
		}
	}
	for role := range sel.Capabilities {
		if !seen[role] {
			return fmt.Errorf("capabilities configured for unselected role %q", role)
		}
	}
	return nil
}

func (s Selection) HasRole(role Role) bool {
	for _, r := range s.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (s Selection) RoleNames() []string {
	out := make([]string, 0, len(s.Roles))
	for _, role := range s.Roles {
		out = append(out, string(role))
	}
	return out
}

func SupportedCapabilities(role Role) []string {
	caps := make([]string, 0, len(allowed[role]))
	for c := range allowed[role] {
		caps = append(caps, c)
	}
	sort.Strings(caps)
	return caps
}
