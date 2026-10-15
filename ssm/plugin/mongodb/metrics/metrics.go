package metrics

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/mongodb"
	"github.com/shatteredsilicon/ssm-client/ssm/utils"
	"gopkg.in/ini.v1"
)

var _ plugin.Metrics = (*Metrics)(nil)

// New returns *Metrics.
func New(dsn string, args []string, cluster string, ssmBaseDir, toolAgentDir string) *Metrics {
	return &Metrics{
		dsn:          dsn,
		args:         args,
		cluster:      cluster,
		ssmBaseDir:   ssmBaseDir,
		toolAgentDir: toolAgentDir,
	}
}

// Metrics implements plugin.Metrics.
type Metrics struct {
	dsn          string
	args         []string
	cluster      string
	ssmBaseDir   string
	toolAgentDir string
	port         int
}

// Init initializes plugin.
func (m *Metrics) Init(
	ctx context.Context,
	ssmUserPassword string,
	bindAddress string,
	authFile string,
	sslKeyFile string,
	sslCertFile string,
) (*plugin.Info, error) {
	info, err := mongodb.Init(ctx, m.dsn, m.args, m.ssmBaseDir)
	if err != nil {
		return nil, err
	}
	m.dsn = info.DSN

	cfgPath := path.Join(m.ssmBaseDir, "mongodb_exporter.conf")
	cfgFile, err := ini.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(cfgFile.Section("web").Key("listen-address").Value(), ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid configuration for web.listen-address")
	}
	port, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("invalid configuration for web.listen-address")
	}
	m.port = int(port)

	cfgFile.Section("mongodb").Key("uri").SetValue(m.dsn)
	cfgFile.Section("web").Key("listen-address").SetValue(fmt.Sprintf("%s:%d", bindAddress, port))
	cfgFile.Section("web").Key("auth-file").SetValue(authFile)
	cfgFile.Section("web").Key("ssl-key-file").SetValue(sslKeyFile)
	cfgFile.Section("web").Key("ssl-cert-file").SetValue(sslCertFile)
	err = cfgFile.SaveTo(cfgPath)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// Name of the exporter.
func (Metrics) Name() string {
	return plugin.NameMongoDB
}

// Port returns bind port.
func (m Metrics) Port() int {
	return m.port
}

// Executable is a name of exporter executable under SSMBaseDir.
func (Metrics) Executable() string {
	return plugin.MongoDBExporter
}

// KV is a list of additional Key-Value data stored in consul.
func (m Metrics) KV() map[string][]byte {
	return map[string][]byte{
		"dsn": []byte(utils.SanitizeDSN(m.dsn)),
	}
}

// Cluster defines cluster name for the target.
func (m Metrics) Cluster() string {
	return m.cluster
}

// CustomOptions returns key-value map of custom options that are applied
func (m Metrics) CustomOptions() (map[string]string, error) {
	return nil, nil
}

// Agents returns agents that are associated to the exporter
func (m Metrics) Agents() ([]plugin.Agent, error) {
	return mongodb.Agents(m.toolAgentDir, m.dsn)
}
