package container

import (
	"log"

	"vozko/usecases/shared/oauthstate"
)

func (c *Container) mustOAuthStateIssuer(keyPrefix, secret, defaultReturnPath string) *oauthstate.Issuer {
	nonces, err := oauthstate.NewNonceStore(c.redisProvider.SharedState(), keyPrefix)
	if err != nil {
		log.Fatalf("[oauth] %s nonce store: %v", keyPrefix, err)
	}
	issuer, err := oauthstate.NewIssuer(secret, nonces, defaultReturnPath)
	if err != nil {
		log.Fatalf("[oauth] %s state issuer: %v", keyPrefix, err)
	}
	return issuer
}
