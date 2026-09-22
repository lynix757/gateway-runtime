package domainapi

import (
	"context"

	"gateway-runtime/internal/appapi"
)

func (c Client) AsAppAPI() appapi.DomainAPI {
	return appAPIAdapter{client: c}
}

type appAPIAdapter struct{ client Client }

func (a appAPIAdapter) GetManagedItem(ctx context.Context, sessionID, id string) (appapi.ManagedItem, error) {
	item, err := a.client.GetManagedItem(ctx, sessionID, id)
	if err != nil {
		return appapi.ManagedItem{}, err
	}
	return appapi.ManagedItem{ID: item.ID, Name: item.Name}, nil
}
