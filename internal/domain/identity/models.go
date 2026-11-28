package identity

import "time"

type Domain struct {
	ID   string
	Name string
}

type User struct {
	ID     string
	Name   string
	Domain Domain
}

type Project struct {
	ID     string
	Name   string
	Domain Domain
}

type Role struct {
	ID   string
	Name string
}

type Fixture struct {
	User    User
	Project Project
	Role    Role
}

type Token struct {
	ID        string
	AuditID   string
	IssuedAt  time.Time
	ExpiresAt time.Time
	Fixture   Fixture

	ApplicationCredential *ApplicationCredential
}

type Selector struct {
	ID         string
	Name       string
	DomainID   string
	DomainName string
}

type Credentials struct {
	User     Selector
	Project  Selector
	Password string
}

type ApplicationCredential struct {
	ID           string
	Name         string
	Description  *string
	UserID       string
	ProjectID    string
	Roles        []Role
	ExpiresAt    *time.Time
	Unrestricted *bool
	SecretSalt   []byte
	SecretHash   []byte
}

func (c ApplicationCredential) Restricted() bool {
	return c.Unrestricted == nil || !*c.Unrestricted
}

type ApplicationCredentialRequest struct {
	Name         string
	Description  *string
	Secret       string
	ExpiresAt    *time.Time
	Unrestricted *bool
	Roles        []Selector
}

type ApplicationCredentialAuth struct {
	ID     string
	Name   string
	User   Selector
	Secret string
}
