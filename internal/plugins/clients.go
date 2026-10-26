package plugins

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"

	"github.com/Orfeo42/ai-orchestration/pluginapi/client"
)

type TokenSource func(name string) (string, bool)

func EnvTokens(name string) (string, bool) {
	return os.LookupEnv(TokenEnv(name))
}

type Clients struct {
	tokens  TokenSource
	hc      *http.Client
	catalog *Catalog
}

func NewClients(tokens TokenSource, hc *http.Client) *Clients {
	return &Clients{tokens: tokens, hc: hc}
}

func (c *Clients) Bind(catalog *Catalog) {
	c.catalog = catalog
}

func (c *Clients) token(name string) (string, error) {
	token, ok := c.tokens(name)
	if !ok || token == "" {
		return "", fmt.Errorf("%w: set %s", ErrNoToken, TokenEnv(name))
	}
	return token, nil
}

func (c *Clients) For(name, baseURL string) (*client.Plugin, error) {
	token, err := c.token(name)
	if err != nil {
		return nil, err
	}
	return client.NewPlugin(baseURL, token, c.hc), nil
}

func (c *Clients) Authenticate(name, token string) error {
	want, err := c.token(name)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnauthorized, err)
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(token)) != 1 {
		return ErrUnauthorized
	}
	return nil
}

func (c *Clients) CancelJob(ctx context.Context, plugin, jobID string) error {
	rec, ok := c.catalog.Plugin(plugin)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, plugin)
	}
	pc, err := c.For(rec.Name, rec.BaseURL)
	if err != nil {
		return err
	}
	if _, err := pc.CancelJob(ctx, jobID); err != nil {
		return fmt.Errorf("cancel job %s on %s: %w", jobID, plugin, err)
	}
	return nil
}
