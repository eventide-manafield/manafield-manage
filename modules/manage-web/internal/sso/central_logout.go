package sso

import (
	"context"
	"errors"
	"net/http"
)

// endCentralSession is an authenticated, server-to-server call. The Bearer
// access token is the proof of the original browser session and is NEVER
// exposed to the browser nor placed in a redirect URL.
func (g *Guard) endCentralSession(ctx context.Context, token string) error {
	if len(token) != 43 || g.cfg.EndSessionURL == "" {
		return errors.New("central logout is not configured for this session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.EndSessionURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return errors.New("Account Core did not confirm central session revocation")
	}
	return nil
}
