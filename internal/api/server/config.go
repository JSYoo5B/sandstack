package server

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Config describes the primary project-scoped fixture, optional additional
// fixtures, and the control endpoint. PublicURL, if set, is the externally
// reachable HTTP or HTTPS origin.
type Config struct {
	Username    string
	Password    string
	ProjectName string
	AdminToken  string
	PublicURL   string
	Fixtures    []Fixture
}

// Fixture is an additional user with the member role on its own project.
type Fixture struct {
	Username    string
	Password    string
	ProjectName string
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
	usernames := map[string]bool{c.Username: true}
	projectNames := map[string]bool{c.ProjectName: true}
	for i, fixture := range c.Fixtures {
		number := i + 1
		if strings.TrimSpace(fixture.Username) == "" || strings.TrimSpace(fixture.ProjectName) == "" ||
			fixture.Password == "" {
			return fmt.Errorf("additional fixture %d username, password, and project name are required", number)
		}
		if fixture.Password == c.AdminToken {
			return fmt.Errorf("SandStack admin token must differ from the password of additional fixture %d", number)
		}
		if usernames[fixture.Username] {
			return fmt.Errorf("additional fixture %d username must be unique", number)
		}
		if projectNames[fixture.ProjectName] {
			return fmt.Errorf("additional fixture %d project name must be unique", number)
		}
		usernames[fixture.Username] = true
		projectNames[fixture.ProjectName] = true
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
