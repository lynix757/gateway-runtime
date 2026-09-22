package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway-runtime/internal/accesspolicy"
	"gateway-runtime/internal/audit"
	"gateway-runtime/internal/auth"
	authoidc "gateway-runtime/internal/auth/oidc"
	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/capability"
	"gateway-runtime/internal/httpx"
	mw "gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/outbound"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/roleruntime"
	"gateway-runtime/internal/runtimecfg"
	"gateway-runtime/internal/session"
	"gateway-runtime/internal/storage"
	"gateway-runtime/internal/storageprovider"
	redisstore "gateway-runtime/internal/store/redis"
	"gateway-runtime/internal/token"
	"gateway-runtime/internal/usercontext"
	"gateway-runtime/internal/workloadauth"
)

type RouteDeps struct {
	Sessions          session.Store
	Policy            policy.Evaluator
	Audit             audit.Sink
	Metrics           *observability.Metrics
	Tokens            outbound.AccessTokenSource
	SessionCookieName string
}

type Options struct {
	BuildAppRoutes func(RouteDeps) (httpx.RouteRegistrar, error)
}

func Run() error {
	return RunWith(Options{})
}

func RunWith(opts Options) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	basePath := cfg.HTTP.NormalizedBasePath()
	if !cfg.Runtime.HasRole(runtimecfg.RoleREBFF) {
		var storageSigner capability.StorageSigner
		storageProviders := storageprovider.Registry{}
		var storageAccessPolicy accesspolicy.Evaluator
		var storageWorkloadVerifier workloadauth.Verifier
		if cfg.Runtime.HasRole(runtimecfg.RoleStorage) && cfg.Storage.SignerBaseURL != "" {
			client, err := outbound.New(cfg.Storage.SignerBaseURL, cfg.Storage.SignerTimeout)
			if err != nil {
				return fmt.Errorf("initialize storage signer client: %w", err)
			}
			storageSigner = storage.RemoteSigner{HTTP: client}
		}
		if cfg.Runtime.HasRole(runtimecfg.RoleStorage) {
			for name, providerCfg := range cfg.Storage.Providers {
				client, err := outbound.New(providerCfg.SignerBaseURL, cfg.Storage.SignerTimeout)
				if err != nil {
					return fmt.Errorf("initialize storage provider %q signer client: %w", name, err)
				}
				storageProviders[name] = storageprovider.Provider{Signer: storage.RemoteSigner{HTTP: client}}
			}
		}
		if cfg.Runtime.HasRole(runtimecfg.RoleStorage) && cfg.Storage.WorkloadAuth.Mode == "oidc" {
			discoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			verifier, err := workloadauth.NewOIDC(discoveryCtx, workloadauth.OIDCConfig{
				Issuer: cfg.Storage.WorkloadAuth.Issuer, Audience: cfg.Storage.WorkloadAuth.Audience,
			})
			cancel()
			if err != nil {
				return fmt.Errorf("initialize storage workload verifier: %w", err)
			}
			storageWorkloadVerifier = verifier
		}
		if cfg.Runtime.HasRole(runtimecfg.RoleStorage) && cfg.Storage.AccessPolicyURL != "" {
			client, err := outbound.New(cfg.Storage.AccessPolicyURL, cfg.Storage.AccessPolicyTimeout)
			if err != nil {
				return fmt.Errorf("initialize storage access policy client: %w", err)
			}
			storageAccessPolicy = accesspolicy.Remote{HTTP: client}
		}
		return roleruntime.Run(roleruntime.Config{
			Addr:                 cfg.HTTP.Addr,
			BasePath:             basePath,
			Selection:            cfg.Runtime,
			StorageSigner:        storageSigner,
			StorageProviders:     storageProviders,
			StorageDefaultTTL:    cfg.Storage.DefaultTTL,
			StorageMaxTTL:        cfg.Storage.MaxTTL,
			StorageTargets:       cfg.Storage.Targets,
			StorageAccessPolicy:  storageAccessPolicy,
			StorageSubjectHeader: cfg.Storage.SubjectHeader,
			WorkloadVerifier:     storageWorkloadVerifier,
			WorkloadAuthRequired: cfg.Storage.WorkloadAuth.Required,
		})
	}
	if len(cfg.Runtime.Roles) > 1 {
		return fmt.Errorf("rebff cannot be combined with specialized roles in v0.1.0")
	}
	publicURL, _ := url.Parse(cfg.HTTP.PublicURL)
	trustedProxy, err := mw.ParseTrustedProxyCIDRs(cfg.HTTP.TrustedProxyCIDRs)
	if err != nil {
		return fmt.Errorf("trusted proxy CIDRs: %w", err)
	}

	var provider auth.IdentityProvider = auth.UnconfiguredProvider{}
	if cfg.OIDC.Issuer != "" {
		discoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p, err := authoidc.New(discoveryCtx, authoidc.Config{
			Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret, Scopes: cfg.OIDC.Scopes,
		})
		cancel()
		if err != nil {
			return fmt.Errorf("initialize oidc provider: %w", err)
		}
		provider = p
	}

	var evaluator policy.Evaluator
	switch cfg.Policy.Provider {
	case "static":
		evaluator = policy.Static{
			RolePermissions:  cfg.Policy.RolePermissions,
			RoleCapabilities: cfg.Policy.RoleCapabilities,
		}
	}

	var (
		flowStore     auth.FlowStore
		sessionStore  session.Store
		tokenStore    token.Store
		userctxCache  cache.Cache
		refreshLocker token.RefreshLocker
		closeStore    func() error
		readiness     httpx.ReadinessCheck
	)

	switch cfg.Store.Backend {
	case "memory":
		flowStore = auth.NewMemoryFlowStore()
		sessionStore = session.NewMemoryStore()
		tokenStore = token.NewMemoryStore()
		userctxCache = cache.NewMemory()
		refreshLocker = token.NewMemoryLocker()
	case "redis":
		client, err := redisstore.New(cfg.Store.RedisURL, cfg.Store.RedisPrefix)
		if err != nil {
			return fmt.Errorf("initialize redis store: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = client.Ping(ctx)
		cancel()
		if err != nil {
			_ = client.Close()
			return fmt.Errorf("redis ping: %w", err)
		}
		flowStore = redisstore.FlowStore{Client: client}
		sessionStore = redisstore.SessionStore{Client: client}
		tokenStore = redisstore.TokenStore{Client: client}
		userctxCache = redisstore.Cache{Client: client}
		refreshLocker = redisstore.RefreshLocker{Client: client}
		closeStore = client.Close
		readiness = client.Ping
	}
	if closeStore != nil {
		defer closeStore()
	}

	sessionStore = session.LifecycleStore{
		Next:          sessionStore,
		MaxLifetime:   cfg.Session.MaxLifetime,
		IdleTimeout:   cfg.Session.IdleTimeout,
		TouchInterval: cfg.Session.TouchInterval,
	}

	authService := &auth.Service{
		Provider: provider, Flows: flowStore, Sessions: sessionStore, Tokens: tokenStore,
		RedirectURI:  cfg.HTTP.PublicEndpoint("/auth/callback"),
		LogoutReturn: cfg.HTTP.PublicEndpoint("/"),
		SessionTTL:   cfg.Session.MaxLifetime,
	}

	baseResolver := usercontext.SessionResolver{
		Sessions: sessionStore,
		Policy:   evaluator,
	}
	userResolver := usercontext.CachedResolver{
		Next:  baseResolver,
		Cache: userctxCache,
		TTL:   usercontext.DefaultTTL,
	}

	var metrics *observability.Metrics
	if cfg.Telemetry.MetricsEnabled {
		metrics = observability.NewMetrics()
	}
	var auditSink audit.Sink = audit.NoopSink{}
	if cfg.Telemetry.AuditEnabled {
		auditSink = audit.SlogSink{}
	}

	tokenManager := token.StoreManager{
		Store:    tokenStore,
		Sessions: sessionStore,
		Locker:   refreshLocker,
	}
	if refresher, ok := provider.(token.Refresher); ok {
		tokenManager.Refresher = refresher
	}

	var appRoutes httpx.RouteRegistrar
	if opts.BuildAppRoutes != nil {
		appRoutes, err = opts.BuildAppRoutes(RouteDeps{
			Sessions:          sessionStore,
			Policy:            evaluator,
			Audit:             auditSink,
			Metrics:           metrics,
			Tokens:            tokenManager,
			SessionCookieName: auth.SessionCookieNameFor(publicURL.Scheme == "https"),
		})
		if err != nil {
			return fmt.Errorf("build application routes: %w", err)
		}
	}

	authHandler := auth.Handler{
		Service:     authService,
		UserContext: userResolver,
		Cache:       userctxCache,
		Audit:       auditSink,
		Metrics:     metrics,
		Secure:      publicURL.Scheme == "https",
		CookieName:  auth.SessionCookieNameFor(publicURL.Scheme == "https"),
	}

	server := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: httpx.NewRouter(basePath, httpx.RouterDeps{
			Auth:                authHandler,
			AppRoutes:           appRoutes,
			Readiness:           readiness,
			PublicURL:           cfg.HTTP.PublicURL,
			SecureCookies:       publicURL.Scheme == "https",
			TrustedProxy:        trustedProxy,
			Metrics:             metrics,
			MaxRequestBodyBytes: cfg.Limits.MaxRequestBodyBytes,
			MaxConcurrency:      cfg.Limits.MaxConcurrency,
			RatePerSecond:       cfg.Limits.RatePerSecond,
			RateBurst:           cfg.Limits.RateBurst,
			RateMaxEntries:      cfg.Limits.RateMaxEntries,
			AuthRatePerSecond:   cfg.Limits.AuthRatePerSecond,
			AuthRateBurst:       cfg.Limits.AuthRateBurst,
			AccessLogEnabled:    cfg.Telemetry.AccessLogEnabled,
			TraceEnabled:        cfg.Telemetry.TraceEnabled,
			SessionCookieName:   auth.SessionCookieNameFor(publicURL.Scheme == "https"),
			CSRFCookieName:      mw.CSRFCookieNameFor(publicURL.Scheme == "https"),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("bff listening",
			"addr", server.Addr,
			"base_path", basePath,
			"public_url", cfg.HTTP.PublicURL,
			"oidc_configured", cfg.OIDC.Issuer != "",
			"store_backend", cfg.Store.Backend,
			"policy_provider", cfg.Policy.Provider,
			"metrics_enabled", cfg.Telemetry.MetricsEnabled,
			"audit_enabled", cfg.Telemetry.AuditEnabled,
			"access_log_enabled", cfg.Telemetry.AccessLogEnabled,
			"trace_enabled", cfg.Telemetry.TraceEnabled,
			"runtime_roles", cfg.Runtime.RoleNames(),
			"runtime_capabilities", cfg.Runtime.Capabilities,
		)
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		slog.Info("shutdown requested", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
