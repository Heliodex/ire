package main

import (
	"time"

	"github.com/heliodex/better-deploy/wt/packages/v2/sign"
)

// signer builds an HMAC signer on the app clock. The window is the same five minutes Ari allows.
func (a *ireApp) signer(secret string) sign.Signer {
	return sign.Signer{
		Secret: []byte(secret),
		Window: time.Duration(signatureWindow) * time.Second,
		Now:    a.now,
	}
}
