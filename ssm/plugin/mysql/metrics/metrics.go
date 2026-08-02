package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/mysql"
	"github.com/shatteredsilicon/ssm-client/ssm/utils"
	"gopkg.in/ini.v1"
)

var _ plugin.Metrics = (*Metrics)(nil)

// Flags are Metrics Metrics specific flags.
type Flags struct {
	DisableTableStats      bool
	DisableTableStatsLimit uint16
	DisableUserStats       bool
	DisableBinlogStats     bool
	DisableProcesslist     bool
}

// disableCollectArgs is a list of optional ssm-admin args to disable mysqld_exporter args.
var disableCollectArgs = map[string]map[string]string{
	"tablestats": {
		"auto_increment.columns":   "0",
		"info_schema.tables":       "0",
		"info_schema.tablestats":   "0",
		"perf_schema.indexiowaits": "0",
		"perf_schema.tableiowaits": "0",
		"perf_schema.tablelocks":   "0",
	},
	"userstats":   {"info_schema.userstats": "0"},
	"binlogstats": {"binlog_size": "0"},
	"processlist": {"info_schema.processlist": "0"},
}

// New returns *Metrics.
func New(flags Flags, mysqlFlags mysql.Flags, ssmBaseDir string) *Metrics {
	return &Metrics{
		flags:      flags,
		mysqlFlags: mysqlFlags,
		ssmBaseDir: ssmBaseDir,
		cfgPath:    path.Join(ssmBaseDir, "mysqld_exporter.conf"),
	}
}

// Metrics implements plugin.Metrics.
type Metrics struct {
	flags      Flags
	mysqlFlags mysql.Flags
	ssmBaseDir string
	port       int
	dsn        string
	cfgPath    string
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
	info, err := mysql.Init(ctx, m.mysqlFlags, ssmUserPassword)
	if err != nil {
		return nil, err
	}
	m.dsn = info.DSN

	cfgFile, err := ini.Load(m.cfgPath)
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

	// updates collect args
	optsToDisable, err := optsToDisable(ctx, m.dsn, m.flags)
	if err != nil {
		return nil, err
	}
	for _, args := range m.collectArgs(optsToDisable) {
		if args == nil {
			continue
		}

		for k, v := range args {
			cfgFile.Section("collect").Key(k).SetValue(v)
		}
	}

	cfgFile.Section("exporter").Key("dsn").SetValue(m.dsn)
	cfgFile.Section("web").Key("auth-file").SetValue(authFile)
	cfgFile.Section("web").Key("ssl-key-file").SetValue(sslKeyFile)
	cfgFile.Section("web").Key("ssl-cert-file").SetValue(sslCertFile)
	cfgFile.Section("exporter").Key("exclude_monitoring_from_slowlog").SetValue(strconv.FormatBool(m.mysqlFlags.ExcludeMonitoring))
	if m.mysqlFlags.SQLCheckTimeout > 0 {
		cfgFile.Section("exporter").Key("sql_check_time").SetValue(m.mysqlFlags.SQLCheckTimeout.String())
	}
	err = cfgFile.SaveTo(m.cfgPath)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// Name of the exporter.
func (m Metrics) Name() string {
	return plugin.NameMySQL
}

// Port returns bind port.
func (m Metrics) Port() int {
	return m.port
}

// collectArgs is a list of additional collect arguments
// that should be updated in config file
func (m Metrics) collectArgs(optsToDisable []string) []map[string]string {
	// Disable exporter options if set so.
	args := make([]map[string]string, 0)
	for _, o := range optsToDisable {
		args = append(args, disableCollectArgs[o])
	}
	return args
}

// Executable is a name of exporter executable under SSMBaseDir.
func (m Metrics) Executable() string {
	return plugin.MySQLExporter
}

// KV is a list of additional Key-Value data stored in consul.
func (m Metrics) KV() map[string][]byte {
	kv := map[string][]byte{}
	kv["dsn"] = []byte(utils.SanitizeDSN(m.dsn))
	return kv
}

// CustomOptions returns key-value map of custom options that are applied
func (m Metrics) CustomOptions() (map[string]string, error) {
	opts := make(map[string]string)

	cfgFile, err := ini.Load(m.cfgPath)
	if err != nil {
		return nil, err
	}

	for opt, collectArgs := range disableCollectArgs {
		matched := true
		for key, value := range collectArgs {
			if utils.CompareINIValues(value, cfgFile.Section("collect").Key(key).Value()) != 0 {
				matched = false
				break
			}
		}

		if matched {
			opts[opt] = "OFF"
		}
	}

	return opts, nil
}

// Cluster defines cluster name for the target.
func (m Metrics) Cluster() string {
	return ""
}

func optsToDisable(ctx context.Context, dsn string, flags Flags) ([]string, error) {
	// Opts to disable.
	var optsToDisable []string
	if !flags.DisableTableStats {
		tableCount, err := tableCount(ctx, dsn)
		if err != nil {
			return nil, err
		}
		// Disable table stats if number of tables is higher than limit.
		if uint16(tableCount) > flags.DisableTableStatsLimit {
			flags.DisableTableStats = true
		}
	}
	if flags.DisableTableStats {
		optsToDisable = append(optsToDisable, "tablestats")
	}
	if flags.DisableUserStats {
		optsToDisable = append(optsToDisable, "userstats")
	}
	if flags.DisableBinlogStats {
		optsToDisable = append(optsToDisable, "binlogstats")
	}
	if flags.DisableProcesslist {
		optsToDisable = append(optsToDisable, "processlist")
	}

	return optsToDisable, nil
}

func tableCount(ctx context.Context, dsn string) (int, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	tableCount := 0
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables").Scan(&tableCount)
	if err != nil {
		return 0, err
	}

	var tablespaceTableName string
	err = db.QueryRowContext(ctx, "SELECT TABLE_NAME FROM information_schema.tables WHERE TABLE_SCHEMA = 'information_schema' AND (TABLE_NAME = 'INNODB_SYS_TABLESPACES' OR TABLE_NAME = 'INNODB_TABLESPACES')").Scan(&tablespaceTableName)
	if err == sql.ErrNoRows {
		return tableCount, nil
	} else if err != nil {
		return 0, err
	}

	tablespaceCount := 0
	err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM information_schema.%s", tablespaceTableName)).Scan(&tablespaceCount)
	if err != nil {
		return 0, err
	}

	if tableCount > tablespaceCount {
		return tableCount, nil
	} else {
		return tablespaceCount, nil
	}
}
