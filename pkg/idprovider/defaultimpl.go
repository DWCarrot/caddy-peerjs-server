package idprovider

import (
	"net/http"

	"github.com/google/uuid"
)

type DefaultClientIdProvider struct {
}

// ValidateClientId implements IClientIdValidator.
func (mgr *DefaultClientIdProvider) ValidateClientId(id string) (bool, error) {
	return true, nil
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
	_ IClientIdProvider  = (*DefaultClientIdProvider)(nil)
	_ IClientIdValidator = (*DefaultClientIdProvider)(nil)
)
