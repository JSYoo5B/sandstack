package compute

type Flavor struct {
	ID           string
	Name         string
	RAMMiB       int
	DiskGiB      int
	VCPUs        int
	EphemeralGiB int
	SwapMiB      int
	Public       bool
	Disabled     bool
	RxTxFactor   float64
}
