package plugin

import (
	"context"
)

// Metrics is a common interface for all exporters.
type Metrics interface {
	// Init initializes plugin and returns Info about database.
	Init(
		ctx context.Context,
		ssmUserPassword string,
		bindAddress string,
		authFile string,
		sslKeyFile string,
		sslCertFile string,
	) (*Info, error)
	// Name of the exporter.
	// As the time of writing this is limited to linux, mysql, mongodb, proxysql and postgresql.
	Name() string
	// Executable is a name of exporter executable under SSMBaseDir.
	Executable() string
	// KV is a list of additional Key-Value data stored in consul.
	KV() map[string][]byte
	// Cluster defines cluster name for the target.
	Cluster() string
	// Port returns bind port.
	Port() int
	// CustomOptions returns key-value map of custom options that are applied
	CustomOptions() (map[string]string, error)
}
