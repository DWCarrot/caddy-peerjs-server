package idprovider

import (
	"net/http"
)

type IClientIdProvider interface {

	// Generate a new unique ID.
	// Return the id.
	// Returns an error if there was a problem generating the ID.
	GenerateClientId(headers http.Header) (string, error)
}
