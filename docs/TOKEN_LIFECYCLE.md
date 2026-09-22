# Token and session lifecycle

M12 separates browser session lifetime from OAuth access-token lifetime.

## Session lifetime

A BFF session is no longer capped by the current access token expiry.

Default runtime values:

- maximum session lifetime: 8h
- idle timeout: 30m
- last-seen touch interval: 1m

Configuration:

    BFF_SESSION_MAX_LIFETIME=8h
    BFF_SESSION_IDLE_TIMEOUT=30m
    BFF_SESSION_TOUCH_INTERVAL=1m

The maximum lifetime is an absolute cap. Activity can refresh LastSeen for idle-timeout purposes but never extends the session beyond the absolute expiry.

## Access-token refresh

The token manager returns the current access token while it is safely before expiry.

By default, a token within 30 seconds of expiry is considered refreshable.

Refresh flow:

    request
      -> read token set
      -> token still fresh? return access token
      -> acquire refresh lock
      -> re-read token set
      -> another replica already refreshed? return new token
      -> refresh with IdP
      -> rotate access/refresh token set
      -> persist using the existing session expiry

The refresh operation never extends the BFF session lifetime.

## Multi-replica locking

Refresh locking is an explicit abstraction.

- memory deployments can use token.MemoryLocker
- Redis deployments must use redisstore.RefreshLocker

The Redis lock uses SET NX with a TTL and ownership-checked delete. Re-reading token state after acquiring the lock prevents multiple BFF replicas from refreshing the same refresh token concurrently.

## Refresh failure

If refresh fails after the access token has reached the refresh window, rebff fails closed:

- token state is deleted
- BFF session is deleted
- the caller cannot fall back to an expired token
- the browser must authenticate again

This covers revoked/invalid refresh tokens and equivalent IdP failures. Applications may later introduce a more detailed provider error taxonomy if they need different behavior for transient transport errors versus invalid_grant.

## ID token

If a refresh response does not return a replacement ID token, the previous server-side ID token is retained for logout hint usage. It is never exposed to the browser.

## Integration coverage

The Keycloak integration flow now also:

1. performs real Authorization Code + PKCE login
2. reads the resulting server-side token set from Redis
3. forces the access-token expiry into the refresh window
4. calls StoreManager with the Keycloak provider and Redis RefreshLocker
5. performs a real refresh-token exchange
6. verifies the refreshed token expiry advances
7. verifies the original BFF session remains valid
8. continues cross-replica logout/session invalidation testing

Unit tests also verify refresh-token rotation, concurrent refresh collapsing to one refresh call, revoked refresh invalidating session, idle timeout, and absolute session lifetime.
