# Runtime Roles and Capabilities Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add runtime role/capability configuration, validation, and composition plumbing so one REBFF binary/image can support `rebff`, `storage`, `streaming`, and `websocket` roles while preserving current REBFF behavior by default.

**Architecture:** Introduce a small role model under `internal/runtimecfg` that owns supported roles, supported capabilities, parsing, normalization, and compatibility validation. `internal/app.Config` consumes that model from environment variables and keeps `rebff` with its current capabilities as the backward-compatible default. The application composition root receives validated runtime selection but this phase does not yet implement storage/streaming/websocket handlers; selecting an unimplemented role fails startup explicitly rather than silently running REBFF.

**Tech Stack:** Go 1.27.1, standard library, existing REBFF config/application packages and test tooling.

**Spec:** `docs/SPEC.md`

## Global Constraints

- Runtime model is `Runtime -> Role -> Capabilities`.
- Initial roles are exactly `rebff`, `storage`, `streaming`, and `websocket`.
- Same codebase, binary, and container image may support all roles.
- Production default remains one workload family per deployment with multiple related capabilities.
- Application/domain services retain final business/data authorization.
- Unsupported role/capability combinations fail startup validation.
- No implicit cross-role dependencies.
- All roles use the canonical audit contract in `docs/AUDIT_CONTRACT.md`; authoritative audit evidence is server-generated.
- Current REBFF startup behavior remains the default when new runtime environment variables are absent.
- No storage/streaming/websocket transport implementation is added in this phase.

## Review Focus

1. Unknown role names must fail with a clear startup error rather than fall back to `rebff`.
2. Unknown capabilities and capabilities belonging to another role must fail startup validation.
3. Duplicate/whitespace-heavy capability input must normalize deterministically and not create duplicate registrations.
4. Empty capability input must use the role's explicit defaults; it must not mean “enable everything” unless the role definition says so.
5. Non-REBFF roles must not accidentally initialize OIDC/session/Redis REBFF dependencies before their actual role implementation exists.

---

### Task 1: Add role/capability domain model

**Files:**
- Create: `internal/runtimecfg/runtime.go`
- Create: `internal/runtimecfg/runtime_test.go`

**Interfaces:**
- Produces:
  - `type Role string`
  - `const RoleREBFF, RoleStorage, RoleStreaming, RoleWebSocket Role`
  - `type Selection struct { Roles []Role; Capabilities map[Role][]string }`
  - `func ParseRoles(string) ([]Role, error)`
  - `func ParseCapabilities([]Role, string) (map[Role][]string, error)`
  - `func Validate(Selection) error`
  - `func DefaultSelection() Selection`
  - `func IsImplemented(Role) bool`

- [ ] **Step 1: Write failing tests for role parsing and defaults**

```go
func TestDefaultSelectionIsREBFF(t *testing.T) {
    got := DefaultSelection()
    if len(got.Roles) != 1 || got.Roles[0] != RoleREBFF {
        t.Fatalf("default roles = %#v", got.Roles)
    }
    want := []string{"oidc", "session", "api-proxy", "user-context"}
    if !reflect.DeepEqual(got.Capabilities[RoleREBFF], want) {
        t.Fatalf("default REBFF capabilities = %#v, want %#v", got.Capabilities[RoleREBFF], want)
    }
}

func TestParseRolesRejectsUnknownRole(t *testing.T) {
    if _, err := ParseRoles("rebff,unknown"); err == nil {
        t.Fatal("expected unknown role to fail")
    }
}

func TestParseRolesNormalizesDuplicates(t *testing.T) {
    got, err := ParseRoles(" storage,storage , streaming ")
    if err != nil {
        t.Fatal(err)
    }
    want := []Role{RoleStorage, RoleStreaming}
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("roles = %#v, want %#v", got, want)
    }
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
/usr/local/go/bin/go test ./internal/runtimecfg
```

Expected: FAIL because `internal/runtimecfg` does not exist.

- [ ] **Step 3: Implement role definitions and parsing**

Create `internal/runtimecfg/runtime.go` with supported-role metadata:

```go
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

var roleCapabilities = map[Role]map[string]struct{}{
    RoleREBFF: set("oidc", "session", "api-proxy", "user-context"),
    RoleStorage: set("upload", "download", "multipart", "presign", "metadata"),
    RoleStreaming: set("playback", "manifest", "token", "origin-select"),
    RoleWebSocket: set("connect", "publish", "subscribe", "presence"),
}

var roleDefaults = map[Role][]string{
    RoleREBFF: {"oidc", "session", "api-proxy", "user-context"},
    RoleStorage: {"upload", "download", "multipart", "presign"},
    RoleStreaming: {"playback", "manifest", "token"},
    RoleWebSocket: {"connect", "publish", "subscribe"},
}

func DefaultSelection() Selection {
    return Selection{
        Roles: []Role{RoleREBFF},
        Capabilities: map[Role][]string{
            RoleREBFF: append([]string(nil), roleDefaults[RoleREBFF]...),
        },
    }
}

func ParseRoles(raw string) ([]Role, error) {
    if strings.TrimSpace(raw) == "" {
        return append([]Role(nil), DefaultSelection().Roles...), nil
    }
    seen := map[Role]bool{}
    var out []Role
    for _, part := range strings.Split(raw, ",") {
        role := Role(strings.ToLower(strings.TrimSpace(part)))
        if _, ok := roleCapabilities[role]; !ok {
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
```

Also add private `set`, normalization helpers, `ParseCapabilities`, `Validate`, and `IsImplemented`.

Capability environment syntax for this phase:

```text
GATEWAY_CAPABILITIES=rebff:oidc|session|api-proxy|user-context;storage:upload|download
```

When exactly one role is selected, shorthand is also accepted:

```text
GATEWAY_ROLE=storage
GATEWAY_CAPABILITIES=upload,download,multipart,presign
```

For multi-role configuration, role-qualified form is mandatory.

- [ ] **Step 4: Add tests for capability validation**

```go
func TestSingleRoleCapabilityShorthand(t *testing.T) {
    roles := []Role{RoleStorage}
    got, err := ParseCapabilities(roles, "upload, download, upload")
    if err != nil {
        t.Fatal(err)
    }
    want := []string{"upload", "download"}
    if !reflect.DeepEqual(got[RoleStorage], want) {
        t.Fatalf("capabilities = %#v, want %#v", got[RoleStorage], want)
    }
}

func TestCrossRoleCapabilityRejected(t *testing.T) {
    roles := []Role{RoleStorage}
    if _, err := ParseCapabilities(roles, "connect"); err == nil {
        t.Fatal("expected websocket capability on storage role to fail")
    }
}

func TestMultiRoleRequiresQualifiedCapabilities(t *testing.T) {
    roles := []Role{RoleStorage, RoleStreaming}
    if _, err := ParseCapabilities(roles, "upload,playback"); err == nil {
        t.Fatal("expected unqualified multi-role capabilities to fail")
    }
}

func TestQualifiedMultiRoleCapabilities(t *testing.T) {
    roles := []Role{RoleStorage, RoleStreaming}
    got, err := ParseCapabilities(roles, "storage:upload|download;streaming:playback|token")
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(got[RoleStorage], []string{"upload", "download"}) {
        t.Fatalf("storage capabilities = %#v", got[RoleStorage])
    }
    if !reflect.DeepEqual(got[RoleStreaming], []string{"playback", "token"}) {
        t.Fatalf("streaming capabilities = %#v", got[RoleStreaming])
    }
}
```

- [ ] **Step 5: Run tests and verify GREEN**

Run:

```bash
/usr/local/go/bin/go test ./internal/runtimecfg
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/runtimecfg/runtime.go internal/runtimecfg/runtime_test.go
git commit -m "feat: add runtime role capability model"
```

---

### Task 2: Integrate runtime selection into application configuration

**Files:**
- Modify: `internal/app/config.go`
- Modify: `internal/app/config_test.go`

**Interfaces:**
- Consumes: `runtimecfg.Selection`, `runtimecfg.ParseRoles`, `runtimecfg.ParseCapabilities`, `runtimecfg.Validate`
- Produces: `Config.Runtime runtimecfg.Selection`
- Environment variables:
  - `GATEWAY_ROLE` — comma-separated role list; defaults to `rebff`
  - `GATEWAY_CAPABILITIES` — role capability selection; defaults to role defaults

- [ ] **Step 1: Add failing config tests**

```go
func TestLoadConfigDefaultsToREBFFRuntime(t *testing.T) {
    t.Setenv("GATEWAY_ROLE", "")
    t.Setenv("GATEWAY_CAPABILITIES", "")
    cfg, err := LoadConfig()
    if err != nil {
        t.Fatal(err)
    }
    if len(cfg.Runtime.Roles) != 1 || cfg.Runtime.Roles[0] != runtimecfg.RoleREBFF {
        t.Fatalf("runtime roles = %#v", cfg.Runtime.Roles)
    }
}

func TestLoadConfigStorageCapabilities(t *testing.T) {
    t.Setenv("GATEWAY_ROLE", "storage")
    t.Setenv("GATEWAY_CAPABILITIES", "upload,download")
    cfg, err := LoadConfig()
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(cfg.Runtime.Capabilities[runtimecfg.RoleStorage], []string{"upload", "download"}) {
        t.Fatalf("runtime capabilities = %#v", cfg.Runtime.Capabilities)
    }
}

func TestLoadConfigRejectsUnknownRuntimeRole(t *testing.T) {
    t.Setenv("GATEWAY_ROLE", "magic")
    if _, err := LoadConfig(); err == nil {
        t.Fatal("expected unsupported runtime role to fail")
    }
}
```

- [ ] **Step 2: Run config tests and verify RED**

Run:

```bash
/usr/local/go/bin/go test ./internal/app
```

Expected: FAIL because `Config.Runtime` does not exist.

- [ ] **Step 3: Add `Runtime` field and environment parsing**

Add to `Config`:

```go
Runtime runtimecfg.Selection
```

In `LoadConfig`, before constructing `Config`:

```go
roles, err := runtimecfg.ParseRoles(os.Getenv("GATEWAY_ROLE"))
if err != nil {
    return Config{}, fmt.Errorf("runtime roles: %w", err)
}
capabilities, err := runtimecfg.ParseCapabilities(roles, os.Getenv("GATEWAY_CAPABILITIES"))
if err != nil {
    return Config{}, fmt.Errorf("runtime capabilities: %w", err)
}
runtimeSelection := runtimecfg.Selection{Roles: roles, Capabilities: capabilities}
if err := runtimecfg.Validate(runtimeSelection); err != nil {
    return Config{}, fmt.Errorf("runtime selection: %w", err)
}
```

Set `Runtime: runtimeSelection` in `cfg`.

- [ ] **Step 4: Keep zero-value test Configs backward-compatible**

Existing unit tests manually build `Config{}`. Do not force them to populate `Runtime`. In `Config.Validate`, validate runtime selection only if `len(c.Runtime.Roles) > 0`. `LoadConfig` always populates the field, so real startup remains strict.

- [ ] **Step 5: Run config tests and full existing app tests**

Run:

```bash
/usr/local/go/bin/go test ./internal/app
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/config.go internal/app/config_test.go
git commit -m "feat: load gateway runtime roles and capabilities"
```

---

### Task 3: Gate the current composition root by implemented runtime role

**Files:**
- Modify: `internal/app/app.go`
- Create: `internal/app/runtime_test.go`

**Interfaces:**
- Consumes: `Config.Runtime`, `runtimecfg.IsImplemented`
- Produces:
  - `func validateImplementedRuntime(runtimecfg.Selection) error`
  - startup log fields `roles` and `capabilities`

- [ ] **Step 1: Write failing tests for current implementation boundary**

```go
func TestValidateImplementedRuntimeAllowsREBFF(t *testing.T) {
    sel := runtimecfg.Selection{
        Roles: []runtimecfg.Role{runtimecfg.RoleREBFF},
        Capabilities: map[runtimecfg.Role][]string{
            runtimecfg.RoleREBFF: {"oidc", "session"},
        },
    }
    if err := validateImplementedRuntime(sel); err != nil {
        t.Fatalf("REBFF runtime rejected: %v", err)
    }
}

func TestValidateImplementedRuntimeRejectsStorageUntilImplemented(t *testing.T) {
    sel := runtimecfg.Selection{
        Roles: []runtimecfg.Role{runtimecfg.RoleStorage},
        Capabilities: map[runtimecfg.Role][]string{
            runtimecfg.RoleStorage: {"upload", "download"},
        },
    }
    if err := validateImplementedRuntime(sel); err == nil {
        t.Fatal("expected storage runtime to fail until role handler is implemented")
    }
}

func TestValidateImplementedRuntimeRejectsMixedRuntimeUntilImplemented(t *testing.T) {
    sel := runtimecfg.Selection{
        Roles: []runtimecfg.Role{runtimecfg.RoleREBFF, runtimecfg.RoleStorage},
        Capabilities: map[runtimecfg.Role][]string{
            runtimecfg.RoleREBFF: {"session"},
            runtimecfg.RoleStorage: {"upload"},
        },
    }
    if err := validateImplementedRuntime(sel); err == nil {
        t.Fatal("expected mixed runtime to fail until all role handlers are implemented")
    }
}
```

- [ ] **Step 2: Run tests and verify RED**

Run:

```bash
/usr/local/go/bin/go test ./internal/app
```

Expected: FAIL because `validateImplementedRuntime` does not exist.

- [ ] **Step 3: Implement explicit role gate before REBFF dependency initialization**

Immediately after `LoadConfig()` succeeds:

```go
if err := validateImplementedRuntime(cfg.Runtime); err != nil {
    return fmt.Errorf("runtime composition: %w", err)
}
```

Implement:

```go
func validateImplementedRuntime(sel runtimecfg.Selection) error {
    for _, role := range sel.Roles {
        if !runtimecfg.IsImplemented(role) {
            return fmt.Errorf("runtime role %q is configured but not implemented in this build", role)
        }
    }
    return nil
}
```

For this phase `IsImplemented` returns true only for `rebff`.

This must occur before OIDC discovery, Redis initialization, session setup, or router composition so non-REBFF selections fail cleanly without initializing unrelated dependencies.

- [ ] **Step 4: Add runtime identity to startup logging**

Extend the existing `slog.Info("bff listening", ...)` fields:

```go
"runtime_roles", cfg.Runtime.Roles,
"runtime_capabilities", cfg.Runtime.Capabilities,
```

Do not change existing log keys in this phase.

- [ ] **Step 5: Run app and integration tests**

Run:

```bash
/usr/local/go/bin/go test ./internal/app ./test/integration
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/runtime_test.go
git commit -m "feat: validate implemented runtime roles at startup"
```

---

### Task 4: Document executable configuration and examples

**Files:**
- Modify: `.env.example` if present
- Modify: `README.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/PRODUCTION.md`

**Interfaces:**
- Documents:
  - `GATEWAY_ROLE`
  - `GATEWAY_CAPABILITIES`
  - current implementation status: only `rebff` starts successfully in this phase

- [ ] **Step 1: Add default runtime variables to environment example**

Add:

```dotenv
GATEWAY_ROLE=rebff
GATEWAY_CAPABILITIES=oidc,session,api-proxy,user-context
```

Include examples as comments:

```dotenv
# Future role examples after their handlers are implemented:
# GATEWAY_ROLE=storage
# GATEWAY_CAPABILITIES=upload,download,multipart,presign
#
# Multi-role syntax:
# GATEWAY_ROLE=storage,streaming
# GATEWAY_CAPABILITIES=storage:upload|download;streaming:playback|token
```

- [ ] **Step 2: Add a README runtime composition section**

Document that role parsing/configuration is implemented now, while specialized role handlers remain future work. Explicitly state that selecting `storage`, `streaming`, or `websocket` currently fails fast at startup.

- [ ] **Step 3: Align architecture/production docs**

Add a short implementation-status note so the spec does not imply specialized roles are already executable.

- [ ] **Step 4: Run documentation-sensitive config tests**

Run:

```bash
/usr/local/go/bin/go test ./internal/runtimecfg ./internal/app
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .env.example README.md docs/ARCHITECTURE.md docs/PRODUCTION.md
git commit -m "docs: describe runtime role configuration"
```

---

### Task 5: Full regression verification

**Files:**
- No product code expected unless verification exposes a regression.

**Interfaces:**
- Verifies all previous task outputs as one release candidate.

- [ ] **Step 1: Format changed Go files**

Run:

```bash
/usr/local/go/bin/gofmt -w internal/runtimecfg/runtime.go internal/runtimecfg/runtime_test.go internal/app/config.go internal/app/config_test.go internal/app/app.go internal/app/runtime_test.go
```

- [ ] **Step 2: Run focused runtime tests**

Run:

```bash
/usr/local/go/bin/go test ./internal/runtimecfg ./internal/app
```

Expected: PASS.

- [ ] **Step 3: Run complete repository tests**

Run:

```bash
/usr/local/go/bin/go test ./...
```

Expected: PASS.

- [ ] **Step 4: Verify backward-compatible default**

Run:

```bash
env -u GATEWAY_ROLE -u GATEWAY_CAPABILITIES /usr/local/go/bin/go test ./test/integration
```

Expected: PASS using `rebff` default selection.

- [ ] **Step 5: Verify fail-fast unsupported role manually through config test path**

Run:

```bash
GATEWAY_ROLE=storage GATEWAY_CAPABILITIES=upload,download /usr/local/go/bin/go test ./internal/app
```

Expected: unit suite PASS; tests confirm config accepts the selection while the composition-root implementation gate rejects attempting to run an unimplemented specialized role.

- [ ] **Step 6: Review changed files**

Run:

```bash
git diff --check
git status --short
```

Expected: no whitespace errors; only intended runtime/config/docs changes.

- [ ] **Step 7: Commit any final verification-only fixes**

```bash
git add internal/runtimecfg internal/app .env.example README.md docs
git commit -m "feat: establish role capability runtime foundation"
```


---

### Follow-up: Canonical Audit Contract Migration

This is a dedicated follow-up after the role/capability foundation.

**Goal:** Replace the current flat `internal/audit.Event` with the canonical audit schema v1.0 while preserving the existing `audit.Sink` abstraction.

**Files:**
- Create: `internal/auditcontract/event.go`
- Create: `internal/auditcontract/validator.go`
- Test: `internal/auditcontract/event_test.go`
- Modify: `internal/audit/audit.go`
- Modify existing audit producers incrementally.

**Required behavior:**
- one schema for REBFF, browser-facing APIs, mobile-facing APIs, and gateway roles;
- server-side authoritative event creation;
- canonical actions independent of client channel;
- request/trace correlation across services;
- shared `client.ip` semantics;
- schema version `1.0`;
- no secret-bearing fields in canonical audit events.
