package idprovider

import (
	"net/http"

	"github.com/google/uuid"
)

type DefaultClientIdProvider struct {
}

// GenerateClientId implements IClientManager.
func (mgr *DefaultClientIdProvider) GenerateClientId(headers http.Header) (string, error) {
	uuid, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return uuid.String(), nil
}

var (
	_ IClientIdProvider = (*DefaultClientIdProvider)(nil)
)
