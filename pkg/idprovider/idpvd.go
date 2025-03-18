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

type IClientIdValidator interface {

	// Validate the client ID.
	// Return true if the client ID is valid, false otherwise.
	// Returns an error if there was a problem validating the ID.
	ValidateClientId(id string) (bool, error)
}
