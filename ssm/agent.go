package ssm

import (
	"context"
	"fmt"
	"os"

	consul "github.com/hashicorp/consul/api"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
)

func (a *Admin) AddAgent(ctx context.Context, agent plugin.Agent) error {
	if err := agent.Init(ctx); err != nil {
		return err
	}

	serviceType := fmt.Sprintf("%s:agent", agent.Name())

	if !a.SkipAdmin && serviceType == plugin.ToolAgent {
		for _, filePath := range agent.GeneratedFiles() {
			if err := os.Chown(filePath, 0, int(a.GID)); err != nil {
				return err
			}
		}
	}

	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return err
	}

	if consulSvc == nil {
		// Add service to Consul.
		srv := consul.AgentService{
			ID:      serviceType,
			Service: serviceType,
			Tags: []string{
				fmt.Sprintf("alias_%s", a.ServiceName),
			},
		}
		reg := consul.CatalogRegistration{
			Node:    a.Config.ClientName,
			Address: a.Config.ClientAddress,
			Service: &srv,
		}
		if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
			return err
		}
	}

	if err := startService(serviceName(serviceType)); err != nil {
		return err
	}

	if err := enableService(serviceName(serviceType)); err != nil {
		return err
	}

	return nil
}

func (a *Admin) RemoveAgent(serviceType string) error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	// Remove service from Consul.
	dereg := consul.CatalogDeregistration{
		Node:      a.Config.ClientName,
		ServiceID: consulSvc.ID,
	}
	if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
		return err
	}

	// Stop and uninstall service.
	if err := stopService(serviceName(serviceType)); err != nil {
		return err
	}

	if err := disableService(serviceName(serviceType)); err != nil {
		return err
	}

	return nil
}
