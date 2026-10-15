/*
	Copyright (c) 2016, Percona LLC and/or its affiliates. All rights reserved.

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU Affero General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU Affero General Public License for more details.

	You should have received a copy of the GNU Affero General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package ssm

import (
	"context"
	"fmt"

	consul "github.com/hashicorp/consul/api"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin/mysql"
	mysqlMetrics "github.com/shatteredsilicon/ssm-client/ssm/plugin/mysql/metrics"
)

var uninitializedMetrics = map[string]plugin.Metrics{
	plugin.MySQLMetrics: mysqlMetrics.New(mysqlMetrics.Flags{}, mysql.Flags{}, SSMBaseDir, ToolAgentBaseDir),
}
var uninitializedAgents = map[string]plugin.Agent{
	plugin.ToolAgent: plugin.NewToolAgent(ToolAgentBaseDir, nil),
}

// AddMetrics add metrics service to monitoring.
func (a *Admin) AddMetrics(ctx context.Context, m plugin.Metrics, force bool, disableSSL bool) (*plugin.Info, error) {
	var sslKeyFile, sslCertFile string
	if !disableSSL {
		// Check and generate certificate if needed.
		if err := a.checkSSLCertificate(); err != nil {
			return nil, err
		}
		sslKeyFile = SSLKeyFile
		sslCertFile = SSLCertFile
	}

	info, err := m.Init(ctx, a.Config.MySQLPassword, a.Config.BindAddress, ConfigFile, sslKeyFile, sslCertFile)
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

	serviceType := fmt.Sprintf("%s:metrics", m.Name())

	addAgentFunc := func() error {
		if agents, err := m.Agents(); err != nil {
			return err
		} else if len(agents) > 0 {
			for _, agent := range agents {
				if err := a.AddAgent(ctx, agent); err != nil {
					return err
				}
				info.Agents = append(info.Agents, fmt.Sprintf("%s:agent", agent.Name()))
			}
		}

		return nil
	}

	consulSvc, err := a.getConsulService(serviceType, "")
	if err != nil {
		return nil, err
	}
	if consulSvc != nil {
		if err = addAgentFunc(); err != nil {
			return nil, err
		}
		return info, ErrDuplicate
	}

	if err := a.checkGlobalDuplicateService(serviceType, a.ServiceName); err != nil {
		return nil, err
	}

	remoteInstanceExists, err := a.remoteInstanceExists(ctx, m.Name(), a.ServiceName)
	if err != nil {
		return nil, err
	}
	if remoteInstanceExists {
		return nil, fmt.Errorf("an %s instance with name %s is already added on server side.", m.Name(), a.ServiceName)
	}

	port := m.Port()
	scheme := "scheme_https"
	if disableSSL {
		scheme = "scheme_http"
	}
	tags := []string{
		fmt.Sprintf("alias_%s", a.ServiceName),
		scheme,
		fmt.Sprintf("region_client"),
		fmt.Sprintf("distro_%s", info.Distro),
		fmt.Sprintf("version_%s", info.Version),
	}
	if m.Cluster() != "" {
		tags = append(tags, fmt.Sprintf("cluster_%s", m.Cluster()))
	}

	// Add service to Consul.
	serviceID := fmt.Sprintf("%s", serviceType)
	srv := consul.AgentService{
		ID:      serviceID,
		Service: serviceType,
		Tags:    tags,
		Port:    port,
	}
	reg := consul.CatalogRegistration{
		Node:    a.Config.ClientName,
		Address: a.Config.ClientAddress,
		Service: &srv,
	}
	if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
		return nil, err
	}

	// Add info to Consul KV.
	for i, v := range m.KV() {
		d := &consul.KVPair{
			Key:   fmt.Sprintf("%s/%s/%s", a.Config.ClientName, serviceID, i),
			Value: v,
		}
		_, err = a.consulAPI.KV().Put(d, nil)
		if err != nil {
			return nil, err
		}
	}

	if err := startService(serviceName(serviceType)); err != nil {
		return nil, err
	}

	if err := enableService(serviceName(serviceType)); err != nil {
		return nil, err
	}

	if err := addAgentFunc(); err != nil {
		return nil, err
	}

	return info, nil
}

// RemoveMetrics remove metrics service from monitoring.
func (a *Admin) RemoveMetrics(name string) error {
	serviceType := fmt.Sprintf("%s:metrics", name)

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

	prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, consulSvc.ID)
	_, err = a.consulAPI.KV().DeleteTree(prefix, nil)
	if err != nil {
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
