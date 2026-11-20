package auth

import (
	_ "embed"
	"encoding/json"

	"github.com/google/uuid"
)

// Provider names by AAGUID, from github.com/passkeydeveloper/passkey-authenticator-aaguids.
//
//go:embed aaguids.json
var aaguidsJSON []byte

var aaguidNames = func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(aaguidsJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

// providerName names a passkey after its provider, or "Passkey" when unknown.
func providerName(aaguid []byte) string {
	id, err := uuid.FromBytes(aaguid)
	if err != nil {
		return "Passkey"
	}
	if n, ok := aaguidNames[id.String()]; ok {
		return n
	}
	return "Passkey"
}
