package server

import (
	"errors"
	"net/url"
	"strings"
)

// Config describes the one project-scoped fixture and its control endpoint.
// PublicURL, if set, is the externally reachable HTTP or HTTPS origin.
type Config struct {
	Username    string
	Password    string
	ProjectName string
	AdminToken  string
	PublicURL   string
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Username) == "" || strings.TrimSpace(c.ProjectName) == "" {
		return errors.New("fixture username and project name are required")
	}
	if c.Password == "" || strings.TrimSpace(c.AdminToken) == "" {
		return errors.New("fixture password and SandStack admin token are required")
	}
	if c.AdminToken != strings.TrimSpace(c.AdminToken) || strings.ContainsAny(c.AdminToken, "\r\n") {
		return errors.New("SandStack admin token must be valid as an HTTP header value")
	}
	if c.AdminToken == c.Password {
		return errors.New("SandStack admin token must differ from the fixture password")
	}
	return nil
}

func publicOrigin(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return nil, nil
	}

	origin, err := url.Parse(rawURL)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.Hostname() == "" || origin.User != nil || origin.ForceQuery ||
		origin.RawQuery != "" || strings.Contains(rawURL, "#") ||
		(origin.EscapedPath() != "" && origin.EscapedPath() != "/") {
		return nil, errors.New("public URL must be an HTTP or HTTPS origin")
	}
	return origin, nil
}
