package compute

const (
	PolicyAffinity     = "affinity"
	PolicyAntiAffinity = "anti-affinity"
)

type ServerGroup struct {
	ID        string
	Name      string
	ProjectID string
	UserID    string
	Policy    string
	Members   []string
}
