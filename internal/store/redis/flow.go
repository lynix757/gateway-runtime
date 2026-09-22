package redisstore

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"gateway-runtime/internal/auth"
)

type FlowStore struct{ Client *Client }

func (s FlowStore) Put(ctx context.Context, flow auth.Flow) error {
	b, err := json.Marshal(flow)
	if err != nil {
		return err
	}
	ttl := time.Until(flow.ExpiresAt)
	if ttl <= 0 {
		return auth.ErrFlowNotFound
	}
	return s.Client.RDB.Set(ctx, s.Client.key("auth", "flow", flow.State), b, ttl).Err()
}

func (s FlowStore) Take(ctx context.Context, state string) (auth.Flow, error) {
	raw, err := s.Client.RDB.GetDel(ctx, s.Client.key("auth", "flow", state)).Bytes()
	if err == goredis.Nil {
		return auth.Flow{}, auth.ErrFlowNotFound
	}
	if err != nil {
		return auth.Flow{}, err
	}
	var flow auth.Flow
	if err := json.Unmarshal(raw, &flow); err != nil {
		return auth.Flow{}, err
	}
	if !flow.ExpiresAt.After(time.Now()) {
		return auth.Flow{}, auth.ErrFlowNotFound
	}
	return flow, nil
}
