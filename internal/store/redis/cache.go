package redisstore

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Cache struct{ Client *Client }

func (c Cache) Get(ctx context.Context, key string, dst any) (bool, error) {
	raw, err := c.Client.RDB.Get(ctx, c.Client.key("cache", key)).Bytes()
	if err == goredis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, err
	}
	return true, nil
}

func (c Cache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Client.RDB.Set(ctx, c.Client.key("cache", key), b, ttl).Err()
}

func (c Cache) Delete(ctx context.Context, key string) error {
	return c.Client.RDB.Del(ctx, c.Client.key("cache", key)).Err()
}
