package app

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"gateway-runtime/internal/runtimecfg"
	"gateway-runtime/internal/storagetarget"
)

type Config struct {
	Runtime   runtimecfg.Selection
	HTTP      HTTPConfig
	Telemetry TelemetryConfig
	Limits    LimitsConfig
	OIDC      OIDCConfig
	Store     StoreConfig
	Policy    PolicyConfig
	Session   SessionConfig
	Storage   StorageRoleConfig
}

type TelemetryConfig struct {
	MetricsEnabled   bool
	AuditEnabled     bool
	AccessLogEnabled bool
	TraceEnabled     bool
}

type LimitsConfig struct {
	MaxRequestBodyBytes int64
	MaxConcurrency      int
	RatePerSecond       float64
	RateBurst           int
	RateMaxEntries      int
	AuthRatePerSecond   float64
	AuthRateBurst       int
}

type HTTPConfig struct {
	Addr              string
	BasePath          string
	PublicURL         string
	TrustedProxyCIDRs []string
}

type StoreConfig struct {
	Backend     string
	RedisURL    string
	RedisPrefix string
}

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

type StorageProviderConfig struct {
	SignerBaseURL string `json:"signer_base_url"`
}

type StorageWorkloadAuthConfig struct {
	Mode     string
	Issuer   string
	Audience string
	Required bool
}

type StorageRoleConfig struct {
	SignerBaseURL       string
	SignerTimeout       time.Duration
	Providers           map[string]StorageProviderConfig
	DefaultTTL          time.Duration
	MaxTTL              time.Duration
	Targets             storagetarget.Registry
	AccessPolicyURL     string
	AccessPolicyTimeout time.Duration
	SubjectHeader       string
	WorkloadAuth        StorageWorkloadAuthConfig
}

type SessionConfig struct {
	MaxLifetime   time.Duration
	IdleTimeout   time.Duration
	TouchInterval time.Duration
}

type PolicyConfig struct {
	Provider         string
	RolePermissions  map[string][]string
	RoleCapabilities map[string][]string
}

func LoadConfig() (Config, error) {
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

	metricsEnabled, err := boolEnv("BFF_METRICS_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	auditEnabled, err := boolEnv("BFF_AUDIT_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	accessLogEnabled, err := boolEnv("BFF_ACCESS_LOG_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	traceEnabled, err := boolEnv("BFF_TRACE_ENABLED", true)
	if err != nil {
		return Config{}, err
	}

	maxLifetime, err := durationEnv("BFF_SESSION_MAX_LIFETIME", 8*time.Hour)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationEnv("BFF_SESSION_IDLE_TIMEOUT", 30*time.Minute)
	if err != nil {
		return Config{}, err
	}
	touchInterval, err := durationEnv("BFF_SESSION_TOUCH_INTERVAL", time.Minute)
	if err != nil {
		return Config{}, err
	}
	storageSignerTimeout, err := durationEnv("STORAGE_SIGNER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	storageDefaultTTL, err := durationEnv("STORAGE_PRESIGN_DEFAULT_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	storageMaxTTL, err := durationEnv("STORAGE_PRESIGN_MAX_TTL", time.Hour)
	if err != nil {
		return Config{}, err
	}
	storageAccessPolicyTimeout, err := durationEnv("STORAGE_ACCESS_POLICY_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}
	storageTargets, err := storagetarget.Parse(os.Getenv("STORAGE_TARGETS_JSON"))
	if err != nil {
		return Config{}, err
	}
	storageProviders, err := parseStorageProviders(os.Getenv("STORAGE_PROVIDERS_JSON"))
	if err != nil {
		return Config{}, err
	}

	maxBody, err := int64Env("BFF_LIMIT_MAX_REQUEST_BODY_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}
	maxConcurrency, err := intEnv("BFF_LIMIT_MAX_CONCURRENCY", 256)
	if err != nil {
		return Config{}, err
	}
	ratePerSecond, err := floatEnv("BFF_LIMIT_RATE_PER_SECOND", 50)
	if err != nil {
		return Config{}, err
	}
	rateBurst, err := intEnv("BFF_LIMIT_RATE_BURST", 100)
	if err != nil {
		return Config{}, err
	}
	rateMaxEntries, err := intEnv("BFF_LIMIT_RATE_MAX_ENTRIES", 10000)
	if err != nil {
		return Config{}, err
	}
	authRatePerSecond, err := floatEnv("BFF_LIMIT_AUTH_RATE_PER_SECOND", 0.2)
	if err != nil {
		return Config{}, err
	}
	authRateBurst, err := intEnv("BFF_LIMIT_AUTH_RATE_BURST", 10)
	if err != nil {
		return Config{}, err
	}

	rolePermissions, err := parseStringSliceMap(os.Getenv("BFF_POLICY_ROLE_PERMISSIONS"))
	if err != nil {
		return Config{}, fmt.Errorf("policy role permissions: %w", err)
	}
	roleCapabilities, err := parseStringSliceMap(os.Getenv("BFF_POLICY_ROLE_CAPABILITIES"))
	if err != nil {
		return Config{}, fmt.Errorf("policy role capabilities: %w", err)
	}

	cfg := Config{
		Runtime: runtimeSelection,
		Telemetry: TelemetryConfig{
			MetricsEnabled:   metricsEnabled,
			AuditEnabled:     auditEnabled,
			AccessLogEnabled: accessLogEnabled,
			TraceEnabled:     traceEnabled,
		},
		Limits: LimitsConfig{
			MaxRequestBodyBytes: maxBody,
			MaxConcurrency:      maxConcurrency,
			RatePerSecond:       ratePerSecond,
			RateBurst:           rateBurst,
			RateMaxEntries:      rateMaxEntries,
			AuthRatePerSecond:   authRatePerSecond,
			AuthRateBurst:       authRateBurst,
		},
		HTTP: HTTPConfig{
			Addr:              envOr("BFF_HTTP_ADDR", ":8080"),
			BasePath:          envOr("BFF_HTTP_BASE_PATH", ""),
			PublicURL:         envOr("BFF_HTTP_PUBLIC_URL", "http://localhost:8080"),
			TrustedProxyCIDRs: splitCSV(os.Getenv("BFF_TRUSTED_PROXY_CIDRS")),
		},
		Store: StoreConfig{
			Backend:     envOr("BFF_STORE_BACKEND", "memory"),
			RedisURL:    os.Getenv("BFF_REDIS_URL"),
			RedisPrefix: envOr("BFF_REDIS_PREFIX", "rebff"),
		},
		OIDC: OIDCConfig{
			Issuer:       os.Getenv("BFF_OIDC_ISSUER"),
			ClientID:     os.Getenv("BFF_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("BFF_OIDC_CLIENT_SECRET"),
			Scopes:       splitCSV(envOr("BFF_OIDC_SCOPES", "openid,profile,email")),
		},
		Session: SessionConfig{MaxLifetime: maxLifetime, IdleTimeout: idleTimeout, TouchInterval: touchInterval},
		Storage: StorageRoleConfig{
			SignerBaseURL:       strings.TrimSpace(os.Getenv("STORAGE_SIGNER_BASE_URL")),
			SignerTimeout:       storageSignerTimeout,
			Providers:           storageProviders,
			DefaultTTL:          storageDefaultTTL,
			MaxTTL:              storageMaxTTL,
			Targets:             storageTargets,
			AccessPolicyURL:     strings.TrimSpace(os.Getenv("STORAGE_ACCESS_POLICY_URL")),
			AccessPolicyTimeout: storageAccessPolicyTimeout,
			SubjectHeader:       envOr("STORAGE_SUBJECT_HEADER", "X-Auth-Subject"),
			WorkloadAuth: StorageWorkloadAuthConfig{
				Mode:     strings.ToLower(strings.TrimSpace(envOr("STORAGE_WORKLOAD_AUTH_MODE", "disabled"))),
				Issuer:   strings.TrimSpace(os.Getenv("STORAGE_WORKLOAD_OIDC_ISSUER")),
				Audience: strings.TrimSpace(os.Getenv("STORAGE_WORKLOAD_OIDC_AUDIENCE")),
				Required: strings.EqualFold(strings.TrimSpace(envOr("STORAGE_WORKLOAD_AUTH_REQUIRED", "false")), "true"),
			},
		},
		Policy: PolicyConfig{
			Provider:         envOr("BFF_POLICY_PROVIDER", "static"),
			RolePermissions:  rolePermissions,
			RoleCapabilities: roleCapabilities,
		},
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if len(c.Runtime.Roles) > 0 {
		if err := runtimecfg.Validate(c.Runtime); err != nil {
			return fmt.Errorf("runtime selection: %w", err)
		}
	}
	if c.HTTP.Addr == "" {
		return fmt.Errorf("http addr is required")
	}
	if _, err := normalizeBasePath(c.HTTP.BasePath); err != nil {
		return err
	}
	u, err := url.Parse(c.HTTP.PublicURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("http public URL must be absolute")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("http public URL must not contain query or fragment")
	}

	if c.Limits.MaxRequestBodyBytes < 0 {
		return fmt.Errorf("max request body bytes must be >= 0")
	}
	if c.Limits.MaxConcurrency < 0 || c.Limits.RatePerSecond < 0 || c.Limits.RateBurst < 0 || c.Limits.RateMaxEntries < 0 || c.Limits.AuthRatePerSecond < 0 || c.Limits.AuthRateBurst < 0 {
		return fmt.Errorf("limits must be >= 0")
	}

	issuerSet := strings.TrimSpace(c.OIDC.Issuer) != ""
	clientSet := strings.TrimSpace(c.OIDC.ClientID) != ""
	if issuerSet != clientSet {
		return fmt.Errorf("oidc issuer and client id must be configured together")
	}

	switch c.Store.Backend {
	case "memory":
	case "redis":
		if strings.TrimSpace(c.Store.RedisURL) == "" {
			return fmt.Errorf("redis URL is required when store backend is redis")
		}
	default:
		return fmt.Errorf("unsupported store backend %q", c.Store.Backend)
	}

	if c.Session.MaxLifetime != 0 || c.Session.IdleTimeout != 0 || c.Session.TouchInterval != 0 {
		if c.Session.MaxLifetime <= 0 {
			return fmt.Errorf("session max lifetime must be positive")
		}
		if c.Session.IdleTimeout <= 0 || c.Session.IdleTimeout > c.Session.MaxLifetime {
			return fmt.Errorf("session idle timeout must be positive and <= max lifetime")
		}
		if c.Session.TouchInterval <= 0 || c.Session.TouchInterval > c.Session.IdleTimeout {
			return fmt.Errorf("session touch interval must be positive and <= idle timeout")
		}
	}

	storageConfigured := c.Runtime.HasRole(runtimecfg.RoleStorage) ||
		c.Storage.SignerBaseURL != "" ||
		len(c.Storage.Providers) > 0 ||
		c.Storage.SignerTimeout != 0 ||
		c.Storage.DefaultTTL != 0 ||
		c.Storage.MaxTTL != 0 ||
		len(c.Storage.Targets) > 0 ||
		c.Storage.AccessPolicyURL != ""
	if storageConfigured {
		if c.Storage.SignerTimeout <= 0 {
			return fmt.Errorf("storage signer timeout must be positive")
		}
		if c.Storage.DefaultTTL <= 0 || c.Storage.MaxTTL <= 0 || c.Storage.DefaultTTL > c.Storage.MaxTTL {
			return fmt.Errorf("storage presign TTLs must be positive and default <= max")
		}
		if c.Storage.SignerBaseURL != "" {
			u, err := url.Parse(c.Storage.SignerBaseURL)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("storage signer base URL must be absolute")
			}
		}
		for name, provider := range c.Storage.Providers {
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("storage provider name is required")
			}
			u, err := url.Parse(strings.TrimSpace(provider.SignerBaseURL))
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("storage provider %q signer base URL must be absolute", name)
			}
		}
		if len(c.Storage.Providers) > 0 {
			for targetName, target := range c.Storage.Targets {
				if _, ok := c.Storage.Providers[target.Provider]; !ok {
					return fmt.Errorf("storage target %q references unconfigured provider %q", targetName, target.Provider)
				}
			}
		}
		if c.Storage.AccessPolicyTimeout <= 0 {
			return fmt.Errorf("storage access policy timeout must be positive")
		}
		if strings.TrimSpace(c.Storage.SubjectHeader) == "" {
			return fmt.Errorf("storage subject header is required")
		}
		if len(c.Storage.Targets) > 0 && c.Storage.AccessPolicyURL == "" {
			return fmt.Errorf("storage access policy URL is required when storage targets are configured")
		}
		if c.Storage.AccessPolicyURL != "" {
			u, err := url.Parse(c.Storage.AccessPolicyURL)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("storage access policy URL must be absolute")
			}
		}
		switch c.Storage.WorkloadAuth.Mode {
		case "", "disabled":
			if c.Storage.WorkloadAuth.Required {
				return fmt.Errorf("storage workload auth cannot be required when mode is disabled")
			}
		case "oidc":
			if strings.TrimSpace(c.Storage.WorkloadAuth.Issuer) == "" || strings.TrimSpace(c.Storage.WorkloadAuth.Audience) == "" {
				return fmt.Errorf("storage workload OIDC issuer and audience are required")
			}
			u, err := url.Parse(c.Storage.WorkloadAuth.Issuer)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("storage workload OIDC issuer must be absolute")
			}
		default:
			return fmt.Errorf("unsupported storage workload auth mode %q", c.Storage.WorkloadAuth.Mode)
		}
	}

	switch c.Policy.Provider {
	case "", "static":
	default:
		return fmt.Errorf("unsupported policy provider %q", c.Policy.Provider)
	}
	return nil
}

func (c HTTPConfig) NormalizedBasePath() string {
	p, _ := normalizeBasePath(c.BasePath)
	return p
}

func (c HTTPConfig) PublicEndpoint(route string) string {
	base, _ := url.Parse(strings.TrimRight(c.PublicURL, "/"))
	base.Path = path.Join(base.Path, c.NormalizedBasePath(), route)
	if strings.HasSuffix(route, "/") && !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	return base.String()
}

func normalizeBasePath(v string) (string, error) {
	if v == "" || v == "/" {
		return "", nil
	}
	if !strings.HasPrefix(v, "/") {
		return "", fmt.Errorf("http base path must start with /")
	}
	for _, segment := range strings.Split(v, "/") {
		if segment == ".." || segment == "." {
			return "", fmt.Errorf("http base path must not contain dot segments")
		}
	}
	cleaned := path.Clean(v)
	if cleaned == "." || cleaned == "/" {
		return "", nil
	}
	return strings.TrimRight(cleaned, "/"), nil
}

func envOr(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return fallback
}

func splitCSV(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parseStorageProviders(v string) (map[string]StorageProviderConfig, error) {
	if strings.TrimSpace(v) == "" {
		return map[string]StorageProviderConfig{}, nil
	}
	var out map[string]StorageProviderConfig
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, fmt.Errorf("parse storage providers: %w", err)
	}
	if out == nil {
		out = map[string]StorageProviderConfig{}
	}
	for name, provider := range out {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return nil, fmt.Errorf("storage provider name is required")
		}
		provider.SignerBaseURL = strings.TrimSpace(provider.SignerBaseURL)
		if provider.SignerBaseURL == "" {
			return nil, fmt.Errorf("storage provider %q signer_base_url is required", trimmed)
		}
		if trimmed != name {
			delete(out, name)
			out[trimmed] = provider
		} else {
			out[name] = provider
		}
	}
	return out, nil
}

func parseStringSliceMap(v string) (map[string][]string, error) {
	if strings.TrimSpace(v) == "" {
		return map[string][]string{}, nil
	}
	var out map[string][]string
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string][]string{}
	}
	return out, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := envOr(name, fallback.String())
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func intEnv(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(envOr(name, strconv.Itoa(fallback)))
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func int64Env(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(envOr(name, strconv.FormatInt(fallback, 10)))
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func floatEnv(name string, fallback float64) (float64, error) {
	raw := strings.TrimSpace(envOr(name, strconv.FormatFloat(fallback, 'g', -1, 64)))
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func boolEnv(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(envOr(name, strconv.FormatBool(fallback)))
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}
