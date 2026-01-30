package ssm

import (
	"context"
	"fmt"
	"net/url"

	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
)

func (a *Admin) AddTuning(ctx context.Context, t plugin.Tuning) (*plugin.Info, error) {
	schema := "http"
	if a.Config.ServerInsecureSSL || a.Config.ServerSSL {
		schema = "https"
	}
	ssmManagedServer := url.URL{
		Scheme: schema,
		Host:   a.Config.ServerAddress,
		Path:   a.Config.ManagedAPIPath,
		User:   url.UserPassword(a.Config.ServerUser, a.Config.ServerPassword),
	}

	info, err := t.Init(ctx, a.Config.MySQLPassword, ssmManagedServer)
	if err != nil {
		return nil, err
	}

	if info.SSMUserPassword != "" {
		a.Config.MySQLPassword = info.SSMUserPassword
		err := a.writeConfig()
		if err != nil {
			return nil, err
		}
	}

	serviceType := fmt.Sprintf("%s:tuning", t.Name())

	if err := startService(serviceName(serviceType)); err != nil {
		return nil, err
	}

	if err := enableService(serviceName(serviceType)); err != nil {
		return nil, err
	}

	return info, nil
}

func (a *Admin) RemoveTuning(name string) error {
	serviceType := fmt.Sprintf("%s:tuning", name)

	if err := stopService(serviceName(serviceType)); err != nil {
		return err
	}

	if err := disableService(serviceName(serviceType)); err != nil {
		return err
	}

	return nil
}
