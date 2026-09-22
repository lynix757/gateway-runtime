package domainapi

import (
	"context"
	"net/http"

	"gateway-runtime/internal/outbound"
)

type Client struct {
	HTTP outbound.JSONDoer
}

type ManagedItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c Client) GetManagedItem(ctx context.Context, sessionID, id string) (ManagedItem, error) {
	var out ManagedItem
	err := c.HTTP.DoJSON(ctx, sessionID, http.MethodGet, "/api/managed-items/"+id, nil, &out)
	return out, err
}
