package queries

import (
	"context"
	"os"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/mysql"
	pc "github.com/shatteredsilicon/ssm/proto/config"
)

var _ plugin.Queries = (*Queries)(nil)

// Flags are MySQL Queries specific flags.
type Flags struct {
	QuerySource string
	// slowlog specific options.
	RetainSlowLogs  int
	SlowLogRotation bool
}

// New returns *Queries.
func New(queriesFlags plugin.QueriesFlags, flags Flags, mysqlFlags mysql.Flags) *Queries {
	return &Queries{
		queriesFlags: queriesFlags,
		flags:        flags,
		mysqlFlags:   mysqlFlags,
	}
}

// Queries implements plugin.Queries.
type Queries struct {
	queriesFlags plugin.QueriesFlags
	flags        Flags
	mysqlFlags   mysql.Flags

	dsn string
}

// Init initializes plugin.
func (q *Queries) Init(ctx context.Context, ssmUserPassword string, info *plugin.Info) (*plugin.Info, error) {
	var err error

	if info == nil {
		info, err = mysql.Init(ctx, q.mysqlFlags, ssmUserPassword)
		if err != nil {
			return nil, err
		}
	}

	if q.flags.QuerySource == "auto" {
		// MySQL is local if the server hostname == MySQL hostname.
		osHostname, _ := os.Hostname()
		if osHostname == info.Hostname {
			q.flags.QuerySource = "slowlog"
		} else {
			q.flags.QuerySource = "perfschema"
		}
	}

	info.QuerySource = q.flags.QuerySource
	q.dsn = info.DSN
	return info, nil
}

// Name of the service.
func (q Queries) Name() string {
	return plugin.NameMySQL
}

// InstanceTypeName of the service.
// Deprecated: QAN API should use the same value as Name().
func (q Queries) InstanceTypeName() string {
	return q.Name()
}

// Config returns pc.QAN.
func (q Queries) Config() pc.QAN {
	exampleQueries := !q.queriesFlags.DisableQueryExamples
	return pc.QAN{
		CollectFrom:    q.flags.QuerySource,
		Interval:       60,
		ExampleQueries: &exampleQueries,
		// "slowlog" specific options.
		SlowLogRotation:  &q.flags.SlowLogRotation,
		RetainSlowLogs:   &q.flags.RetainSlowLogs,
		PrefetchMetadata: &q.queriesFlags.PrefetchMetadata,
		PrefetchExplain:  &q.queriesFlags.PrefetchExplain,
	}
}

// FilterOmit returns queries that should be omitted
func (q Queries) FilterOmit() []string {
	return q.mysqlFlags.FilterOmit
}
