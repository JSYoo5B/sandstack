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
