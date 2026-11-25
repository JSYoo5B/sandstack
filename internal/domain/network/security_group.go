package network

type SecurityGroup struct {
	ID          string
	Name        string
	Description string
	ProjectID   string
	Stateful    bool
	Rules       []SecurityGroupRule
}

type SecurityGroupRule struct {
	ID            string
	Direction     string
	EtherType     string
	RemoteGroupID string
}
