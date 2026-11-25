package network

type Network struct {
	ID        string
	Name      string
	ProjectID string
	AdminDown bool
	Down      bool
}

func (n Network) Status() string {
	if n.Down || n.AdminDown {
		return "DOWN"
	}
	return "ACTIVE"
}
