package queries

import (
	"context"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/postgresql"
	"github.com/shatteredsilicon/ssm-client/ssm/utils"
	pc "github.com/shatteredsilicon/ssm/proto/config"
)

var _ plugin.Queries = (*Queries)(nil)

// Flags are PostgreSQL Queries specific flags.
type Flags struct {
	QuerySource string
}

// New returns *Queries.
func New(queriesFlags plugin.QueriesFlags, flags Flags, pgFlags postgresql.Flags) *Queries {
	return &Queries{
		queriesFlags: queriesFlags,
		flags:        flags,
		pgFlags:      pgFlags,
	}
}

// Queries implements plugin.Queries.
type Queries struct {
	queriesFlags plugin.QueriesFlags
	flags        Flags
	pgFlags      postgresql.Flags

	dsn string
}

// Init initializes plugin.
func (q *Queries) Init(ctx context.Context, ssmUserPassword string, info *plugin.Info) (*plugin.Info, error) {
	var err error

	if info == nil {
		info, err = postgresql.Init(ctx, q.pgFlags, ssmUserPassword)
		if err != nil {
			return nil, err
		}
	}

	if q.flags.QuerySource == "auto" {
		// PostgreSQL is local if inet_server_addr is empty or localhost/127.0.0.1
		if info.Hostname == "" || utils.SliceContains([]string{"localhost", "127.0.0.1", "::1"}, info.Hostname) {
			q.flags.QuerySource = "logfile"
		} else {
			q.flags.QuerySource = "table"
		}
	}

	info.QuerySource = q.flags.QuerySource
	q.dsn = info.DSN
	return info, nil
}

// Name of the service.
func (q Queries) Name() string {
	return plugin.NamePostgreSQL
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
	}
}
