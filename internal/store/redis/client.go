package redisstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	RDB    *redis.Client
	Prefix string
}

func New(rawURL, prefix string) (*Client, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("redis URL is required")
	}
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	if prefix == "" {
		prefix = "rebff"
	}
	return &Client{RDB: redis.NewClient(opts), Prefix: strings.TrimSuffix(prefix, ":")}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.RDB.Ping(ctx).Err()
}

func (c *Client) Close() error {
	return c.RDB.Close()
}

func (c *Client) key(parts ...string) string {
	all := append([]string{c.Prefix}, parts...)
	return strings.Join(all, ":")
}
