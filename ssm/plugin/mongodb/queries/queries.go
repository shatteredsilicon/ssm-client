package queries

import (
	"context"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/mongodb"
	pc "github.com/shatteredsilicon/ssm/proto/config"
)

var _ plugin.Queries = (*Queries)(nil)

// New returns *Queries.
func New(queriesFlags plugin.QueriesFlags, dsn string, args []string, pmmBaseDir string) *Queries {
	return &Queries{
		queriesFlags: queriesFlags,
		dsn:          dsn,
		args:         args,
		pmmBaseDir:   pmmBaseDir,
	}
}

// Queries implements plugin.Queries.
type Queries struct {
	queriesFlags plugin.QueriesFlags
	dsn          string
	args         []string
	pmmBaseDir   string
}

// Init initializes plugin.
func (q *Queries) Init(ctx context.Context, ssmUserPassword string, _ *plugin.Info) (*plugin.Info, error) {
	info, err := mongodb.Init(ctx, q.dsn, q.args, q.pmmBaseDir)
	if err != nil {
		return nil, err
	}
	q.dsn = info.DSN
	return info, nil
}

// Name of the service.
func (q Queries) Name() string {
	return plugin.NameMongoDB
}

// InstanceTypeName of the service.
// Deprecated: QAN API should use `mongodb` not `mongo`.
func (q Queries) InstanceTypeName() string {
	return "mongo"
}

// Config returns pc.QAN.
func (q Queries) Config() pc.QAN {
	exampleQueries := !q.queriesFlags.DisableQueryExamples
	return pc.QAN{
		ExampleQueries:   &exampleQueries,
		PrefetchMetadata: &q.queriesFlags.PrefetchMetadata,
	}
}
