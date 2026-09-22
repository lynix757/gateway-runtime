package asset

import (
	"context"
	"net/http"

	"gateway-runtime/core"
)

type JSONDoer interface {
	DoJSON(ctx context.Context, sessionID, method, path string, in, out any) error
}

type Asset struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Serial    string `json:"serial"`
	Status    string `json:"status"`
	ProjectID string `json:"project_id,omitempty"`
}

type Client struct {
	HTTP JSONDoer
}

func (c Client) Get(ctx context.Context, sessionID, id string) (Asset, error) {
	var out Asset
	err := c.HTTP.DoJSON(ctx, sessionID, http.MethodGet, "/api/assets/"+id, nil, &out)
	return out, err
}

var _ JSONDoer = (*core.JSONClient)(nil)
