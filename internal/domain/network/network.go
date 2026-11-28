package network

type Network struct {
	ID        string
	Name      string
	ProjectID string
	AdminDown bool
	Down      bool
	External  bool
	SubnetIDs []string
}

func (n Network) Status() string {
	if n.Down || n.AdminDown {
		return "DOWN"
	}
	return "ACTIVE"
}
