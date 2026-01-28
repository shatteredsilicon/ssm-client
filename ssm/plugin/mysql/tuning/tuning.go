package tuning

import (
	"context"
	"net/url"
	"path"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/mysql"
	"gopkg.in/ini.v1"
)

type Tuning struct {
	flags      Flags
	mysqlFlags mysql.Flags
	cfgPath    string
	dsn        string
}

type Flags struct{}

// New returns *Metrics.
func New(flags Flags, mysqlFlags mysql.Flags, ssmBaseDir string) *Tuning {
	return &Tuning{
		flags:      flags,
		mysqlFlags: mysqlFlags,
		cfgPath:    path.Join(ssmBaseDir, "mt-agent.conf"),
	}
}

func (t *Tuning) Name() string {
	return plugin.NameMySQL
}

func (t *Tuning) Init(ctx context.Context, ssmUserPassword string, ssmManagedServer url.URL) (*plugin.Info, error) {
	info, err := mysql.Init(ctx, t.mysqlFlags, ssmUserPassword)
	if err != nil {
		return nil, err
	}
	t.dsn = info.DSN

	cfgFile, err := ini.LooseLoad(t.cfgPath)
	if err != nil {
		return nil, err
	}

	cfgFile.Section("mysql").Key("dsn").SetValue(t.dsn)
	cfgFile.Section("server").Key("url").SetValue(ssmManagedServer.String())

	if err = cfgFile.SaveTo(t.cfgPath); err != nil {
		return nil, err
	}

	return info, nil
}
