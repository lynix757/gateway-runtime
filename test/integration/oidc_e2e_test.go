package integration

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"gateway-runtime/internal/auth"
	authoidc "gateway-runtime/internal/auth/oidc"
	"gateway-runtime/internal/cache"
	"gateway-runtime/internal/httpx"
	mw "gateway-runtime/internal/httpx/middleware"
	"gateway-runtime/internal/observability"
	"gateway-runtime/internal/policy"
	"gateway-runtime/internal/session"
	redisstore "gateway-runtime/internal/store/redis"
	"gateway-runtime/internal/token"
	"gateway-runtime/internal/usercontext"
)

var loginFormActionRE = regexp.MustCompile(`(?s)<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`)

func TestOIDCLoginAndLogoutAcrossReplicas(t *testing.T) {
	if testing.Short() || getenv("REBFF_INTEGRATION") != "1" {
		t.Skip("set REBFF_INTEGRATION=1 to run integration tests")
	}

	ctx := context.Background()
	const (
		redisURL = "redis://localhost:16379/0"
		issuer   = "http://localhost:18081/realms/rebff"
		prefix   = "rebff-it-e2e"
	)

	provider, err := authoidc.New(ctx, authoidc.Config{
		Issuer:       issuer,
		ClientID:     "rebff-bff",
		ClientSecret: "rebff-dev-secret",
		Scopes:       []string{"openid", "profile", "email"},
	})
	if err != nil {
		t.Fatalf("initialize OIDC provider: %v", err)
	}

	clientA, err := redisstore.New(redisURL, prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer clientA.Close()
	clientB, err := redisstore.New(redisURL, prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer clientB.Close()

	if err := clientA.Ping(ctx); err != nil {
		t.Fatalf("redis unavailable: %v", err)
	}
	if err := clientA.RDB.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	srvA := startOIDCReplica(t, "127.0.0.1:18080", "http://localhost:18080", provider, clientA)
	defer srvA()
	srvB := startOIDCReplica(t, "127.0.0.1:18082", "http://localhost:18082", provider, clientB)
	defer srvB()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
	}

	resp, err := httpClient.Get("http://localhost:18080/auth/login?return_to=/")
	if err != nil {
		t.Fatalf("start login: %v", err)
	}
	loginHTML, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("keycloak login page status = %d", resp.StatusCode)
	}

	match := loginFormActionRE.FindSubmatch(loginHTML)
	if len(match) != 2 {
		t.Fatalf("Keycloak login form action not found")
	}
	action := html.UnescapeString(string(match[1]))

	form := url.Values{
		"username": {"alice"},
		"password": {"alice-password"},
	}
	resp, err = httpClient.PostForm(action, form)
	if err != nil {
		t.Fatalf("submit Keycloak login: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	var me usercontext.Context
	getJSON(t, httpClient, "http://localhost:18080/api/me", http.StatusOK, &me)
	if me.Subject == "" || me.Email != "alice@example.com" {
		t.Fatalf("unexpected /api/me identity: %+v", me)
	}
	if !contains(me.Roles, "admin") || !contains(me.Permissions, "managed-item.read") || !contains(me.Capabilities, "asset-admin") {
		t.Fatalf("unexpected /api/me authorization context: %+v", me)
	}

	var meReplicaB usercontext.Context
	getJSON(t, httpClient, "http://localhost:18082/api/me", http.StatusOK, &meReplicaB)
	if meReplicaB.Subject != me.Subject {
		t.Fatalf("replica B subject = %q, replica A subject = %q", meReplicaB.Subject, me.Subject)
	}

	baseA, _ := url.Parse("http://localhost:18080")
	sessionID := cookieValue(jar.Cookies(baseA), auth.SessionCookieName)
	if sessionID == "" {
		t.Fatal("session cookie missing")
	}
	sessionStore := redisstore.SessionStore{Client: clientA}
	tokenStore := redisstore.TokenStore{Client: clientA}
	currentSession, err := sessionStore.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	currentTokens, err := tokenStore.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	currentTokens.ExpiryUnix = time.Now().Add(time.Second).Unix()
	if err := tokenStore.Put(ctx, sessionID, currentTokens, currentSession.ExpiresAt); err != nil {
		t.Fatal(err)
	}

	manager := token.StoreManager{
		Store:         tokenStore,
		Sessions:      sessionStore,
		Refresher:     provider,
		Locker:        redisstore.RefreshLocker{Client: clientA},
		RefreshBefore: 30 * time.Second,
	}
	if _, err := manager.AccessToken(ctx, sessionID); err != nil {
		t.Fatalf("refresh access token: %v", err)
	}
	refreshedTokens, err := tokenStore.Get(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedTokens.ExpiryUnix <= time.Now().Add(30*time.Second).Unix() {
		t.Fatalf("refreshed token expiry not advanced: %d", refreshedTokens.ExpiryUnix)
	}
	if _, err := sessionStore.Get(ctx, sessionID); err != nil {
		t.Fatalf("session should remain valid after token refresh: %v", err)
	}

	baseB, _ := url.Parse("http://localhost:18082")
	csrf := cookieValue(jar.Cookies(baseB), mw.CSRFCookieName)
	if csrf == "" {
		t.Fatal("CSRF cookie missing")
	}

	req, err := http.NewRequest(http.MethodPost, "http://localhost:18082/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://localhost:18082")
	req.Header.Set(mw.CSRFHeaderName, csrf)
	resp, err = httpClient.Do(req)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()

	getJSON(t, httpClient, "http://localhost:18080/api/me", http.StatusUnauthorized, nil)
}

func startOIDCReplica(t *testing.T, addr, publicURL string, provider auth.IdentityProvider, client *redisstore.Client) func() {
	t.Helper()

	flows := redisstore.FlowStore{Client: client}
	sessions := redisstore.SessionStore{Client: client}
	tokens := redisstore.TokenStore{Client: client}
	userCache := redisstore.Cache{Client: client}
	evaluator := policy.Static{
		RolePermissions: map[string][]string{
			"admin":  {"managed-item.read", "storage.upload"},
			"viewer": {"managed-item.read"},
		},
		RoleCapabilities: map[string][]string{
			"admin": {"asset-admin"},
		},
	}

	service := &auth.Service{
		Provider:     provider,
		Flows:        flows,
		Sessions:     sessions,
		Tokens:       tokens,
		RedirectURI:  publicURL + "/auth/callback",
		LogoutReturn: publicURL + "/",
	}
	resolver := usercontext.CachedResolver{
		Next: usercontext.SessionResolver{
			Sessions: sessions,
			Policy:   evaluator,
		},
		Cache: userCache,
		TTL:   usercontext.DefaultTTL,
	}
	handler := auth.Handler{
		Service:     service,
		UserContext: resolver,
		Cache:       userCache,
		Metrics:     observability.NewMetrics(),
		Secure:      false,
	}

	router := httpx.NewRouter("", httpx.RouterDeps{
		Auth:          handler,
		PublicURL:     publicURL,
		SecureCookies: false,
		Metrics:       observability.NewMetrics(),
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	server := &http.Server{Handler: router}
	go func() {
		_ = server.Serve(ln)
	}()

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}
}

func getJSON(t *testing.T, client *http.Client, rawURL string, wantStatus int, out any) {
	t.Helper()
	resp, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("GET %s status = %d, want %d; body=%s", rawURL, resp.StatusCode, wantStatus, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func cookieValue(cookies []*http.Cookie, name string) string {
	for _, c := range cookies {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func getenv(name string) string {
	return strings.TrimSpace(getenvRaw(name))
}

var getenvRaw = func(name string) string {
	return os.Getenv(name)
}

var (
	_ cache.Cache   = redisstore.Cache{}
	_ session.Store = redisstore.SessionStore{}
	_ token.Store   = redisstore.TokenStore{}
)
