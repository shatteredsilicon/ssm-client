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
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docker/cli/templates"
	"github.com/fatih/color"
	consul "github.com/hashicorp/consul/api"
	service "github.com/percona/kardianos-service"
	"github.com/prometheus/client_golang/api/prometheus"
	"github.com/shatteredsilicon/ssm/version"

	"github.com/shatteredsilicon/ssm-client/ssm/managed"
	"github.com/shatteredsilicon/ssm-client/ssm/plugin"
	"github.com/shatteredsilicon/ssm-client/ssm/utils"
	"gopkg.in/ini.v1"
)

// Admin main class.
type Admin struct {
	ServiceName  string
	ServicePort  int
	Args         []string // Args defines additional arguments to pass through to *_exporter or qan-agent
	Config       *Config
	Verbose      bool
	SkipAdmin    bool
	Format       string
	serverURL    string
	apiTimeout   time.Duration
	qanAPI       *API
	consulAPI    *consul.Client
	promQueryAPI prometheus.QueryAPI
	managedAPI   *managed.Client
	//promSeriesAPI prometheus.SeriesAPI
}

// SetAPI setups QAN, Consul, Prometheus, pmm-managed clients and verifies connections.
func (a *Admin) SetAPI() error {
	// Set default API timeout if unset.
	if a.apiTimeout == 0 {
		a.apiTimeout = apiTimeout
	}

	scheme := "http"
	helpText := ""
	insecureTransport := &http.Transport{}
	if a.Config.ServerInsecureSSL {
		scheme = "https"
		insecureTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		helpText = "--server-insecure-ssl"
	}
	if a.Config.ServerSSL {
		scheme = "https"
		helpText = "--server-ssl"
	}

	// QAN API.
	a.qanAPI = NewAPI(a.Config.ServerInsecureSSL, a.apiTimeout, a.Verbose)
	httpClient := a.qanAPI.NewClient()

	// Consul API.
	config := consul.Config{
		Address:    a.Config.ServerAddress,
		HttpClient: httpClient,
		Scheme:     scheme,
	}
	var authStr string
	if a.Config.ServerUser != "" {
		config.HttpAuth = &consul.HttpBasicAuth{
			Username: a.Config.ServerUser,
			Password: a.Config.ServerPassword,
		}
		authStr = fmt.Sprintf("%s:%s@", url.QueryEscape(a.Config.ServerUser), url.QueryEscape(a.Config.ServerPassword))
	}
	a.consulAPI, _ = consul.NewClient(&config)

	// Full URL.
	a.serverURL = fmt.Sprintf("%s://%s%s", scheme, authStr, a.Config.ServerAddress)

	// Prometheus API.
	cfg := prometheus.Config{Address: fmt.Sprintf("%s/prometheus", a.serverURL)}
	// cfg.Transport = httpClient.Transport
	// above should be used instead below but
	// https://github.com/prometheus/client_golang/issues/292
	if a.Config.ServerInsecureSSL {
		cfg.Transport = insecureTransport
	}
	client, _ := prometheus.New(cfg)
	a.promQueryAPI = prometheus.NewQueryAPI(client)
	//a.promSeriesAPI = prometheus.NewSeriesAPI(client)

	// Check if server is alive.
	qanApiURL := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "ping")
	resp, _, err := a.qanAPI.Get(qanApiURL)
	if err != nil {
		if strings.Contains(err.Error(), "x509: cannot validate certificate") {
			return fmt.Errorf(`Unable to connect to SSM server by address: %s

Looks like SSM server running with self-signed SSL certificate.
Run 'ssm-admin config --server-insecure-ssl' to enable such configuration.`, a.Config.ServerAddress)
		}
		serverURL := fmt.Sprintf("%s://%s", scheme, a.Config.ServerAddress)
		cleanedErr := strings.Replace(err.Error(), a.serverURL, serverURL, -1)
		return fmt.Errorf(`Unable to connect to SSM server by address: %s
%s

* Check if the configured address is correct.
* If server is running on non-default port, ensure it was specified along with the address.
* If server is enabled for SSL or self-signed SSL, enable the corresponding option.
* You may also check the firewall settings.`, a.Config.ServerAddress, cleanedErr)
	}

	// Try to detect 400 (SSL) and 401 (HTTP auth).
	if resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf(`Unable to connect to SSM server by address: %s

Looks like the server is enabled for SSL or self-signed SSL.
Use 'ssm-admin config' to enable the corresponding SSL option.`, a.Config.ServerAddress)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf(`Unable to connect to SSM server by address: %s

Looks like the server is password protected.
Use 'ssm-admin config' to define server user and password.`, a.Config.ServerAddress)
	}

	// Check Consul status.
	if leader, err := a.consulAPI.Status().Leader(); err != nil || leader == "" {
		return fmt.Errorf(`Unable to connect to SSM server by address: %s

Even though the server is reachable it does not look to be SSM server.
Check if the configured address is correct. %s`, a.Config.ServerAddress, err)
	}

	// Check if server is not password protected but client is configured so.
	if a.Config.ServerUser != "" {
		serverURL := fmt.Sprintf("%s://%s", scheme, a.Config.ServerAddress)
		qanApiURL = a.qanAPI.URL(serverURL, qanAPIBasePath, "ping")
		if resp, _, err := a.qanAPI.Get(qanApiURL); err == nil && resp.StatusCode == http.StatusOK {
			return fmt.Errorf(`This client is configured with HTTP basic authentication.
However, SSM server is not.

If you forgot to enable password protection on the server, you may want to do so.

Otherwise, run the following command to reset the config and disable authentication:
ssm-admin config --server %s %s`, a.Config.ServerAddress, helpText)
		}
	}

	var user *url.Userinfo
	if a.Config.ServerUser != "" {
		user = url.UserPassword(a.Config.ServerUser, a.Config.ServerPassword)
	}
	a.managedAPI = managed.NewClient(a.Config.ServerAddress, a.Config.ManagedAPIPath, scheme, user, a.Config.ServerInsecureSSL, a.Verbose)

	return nil
}

// PrintInfo print SSM client info.
func (a *Admin) PrintInfo() {
	fmt.Printf("ssm-admin %s\n\n", Version)
	a.ServerInfo()
	fmt.Printf("%-15s | %s\n\n", "Service Manager", service.Platform())

	fmt.Printf("%-15s | %s\n", "Go Version", strings.Replace(runtime.Version(), "go", "", 1))
	fmt.Printf("%-15s | %s/%s\n\n", "Runtime Info", runtime.GOOS, runtime.GOARCH)
}

const (
	ServerInfoTemplate = `{{define "ServerInfo"}}{{printf "%-15s | %s %s" "SSM Server" .ServerAddress .ServerSecurity}}
{{printf "%-15s | %s" "Client Name" .ClientName}}
{{printf "%-15s | %s %s" "Client Address" .ClientAddress .ClientBindAddress}}{{end}}`

	DefaultServerInfoTemplate = `{{template "ServerInfo" .}}
`
)

type ServerInfo struct {
	ServerAddress     string `json:"server_address"`
	ServerSecurity    string `json:"server_security"`
	ClientName        string `json:"client_name"`
	ClientAddress     string `json:"client_address"`
	ClientBindAddress string `json:"client_bind_address"`
}

// ServerInfo print server info.
func (a *Admin) ServerInfo() error {
	serverInfo := a.serverInfo()

	tmpl, err := templates.Parse(DefaultServerInfoTemplate)
	if err != nil {
		return err
	}
	tmpl, err = tmpl.Parse(ServerInfoTemplate)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(os.Stdout, serverInfo); err != nil {
		return err
	}

	return nil
}

func (a *Admin) serverInfo() ServerInfo {
	var labels []string
	if a.Config.ServerInsecureSSL {
		labels = append(labels, "insecure SSL")
	} else if a.Config.ServerSSL {
		labels = append(labels, "SSL")
	}
	if a.Config.ServerUser != "" {
		labels = append(labels, "password-protected")
	}
	securityInfo := ""
	if len(labels) > 0 {
		securityInfo = fmt.Sprintf("(%s)", strings.Join(labels, ", "))
	}

	bindAddress := ""
	if a.Config.ClientAddress != a.Config.BindAddress {
		bindAddress = fmt.Sprintf("(%s)", a.Config.BindAddress)
	}

	return ServerInfo{
		ServerAddress:     a.Config.ServerAddress,
		ServerSecurity:    securityInfo,
		ClientName:        a.Config.ClientName,
		ClientAddress:     a.Config.ClientAddress,
		ClientBindAddress: bindAddress,
	}
}

// StartStopMonitoring start/stop system service by its metric type and name.
func (a *Admin) StartStopMonitoring(action, svcType string) (affected bool, err error) {
	err = isValidSvcType(svcType)
	if err != nil {
		return false, err
	}

	// Check if we have this service on Consul.
	if !IsOfflineAction(action) {
		consulSvc, err := a.getConsulService(svcType, a.ServiceName)
		if err != nil {
			return false, err
		}
		if consulSvc == nil {
			return false, ErrNoService
		}
	}

	var svcName string
	services := GetLocalServices(svcType)
	if len(services) > 0 {
		svcName = services[0].serviceName
	}
	switch action {
	case "start":
		if getServiceStatus(svcName) {
			// if it's already started then return
			return false, nil
		}
		if err := startService(svcName); err != nil {
			return false, err
		}
	case "stop":
		if !getServiceStatus(svcName) {
			// if it's already stopped then return
			return false, nil
		}
		if err := stopService(svcName); err != nil {
			return false, err
		}
	case "restart":
		if err := stopService(svcName); err != nil {
			return false, err
		}
		if err := startService(svcName); err != nil {
			return false, err
		}
	case "enable":
		if err := enableService(svcName); err != nil {
			return false, err
		}
	case "disable":
		if err := disableService(svcName); err != nil {
			return false, err
		}
	}

	return true, nil
}

// StartStopAllMonitoring start/stop all metric services.
func (a *Admin) StartStopAllMonitoring(action string) (numOfAffected, numOfAll int, err error) {
	var errs Errors

	localServices := GetLocalServices()
	numOfAll = len(localServices)

	for _, svc := range localServices {
		if !IsOfflineAction(action) {
			consulSvc, err := a.getConsulService(svc.serviceType, a.ServiceName)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if consulSvc == nil {
				continue
			}
		}

		switch action {
		case "start":
			if getServiceStatus(svc.serviceName) {
				// if it's already started then continue
				continue
			}
			if err := startService(svc.serviceName); err != nil {
				errs = append(errs, err)
				continue
			}
		case "stop":
			if !getServiceStatus(svc.serviceName) {
				// if it's already stopped then continue
				continue
			}
			if err := stopService(svc.serviceName); err != nil {
				errs = append(errs, err)
				continue
			}
		case "restart":
			if err := stopService(svc.serviceName); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := startService(svc.serviceName); err != nil {
				errs = append(errs, err)
				continue
			}
		case "enable":
			if err := enableService(svc.serviceName); err != nil {
				errs = append(errs, err)
				continue
			}
		case "disable":
			if err := disableService(svc.serviceName); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		numOfAffected++
	}

	if len(errs) > 0 {
		return numOfAffected, numOfAll, errs
	}

	return numOfAffected, numOfAll, nil
}

// RemoveAllMonitoring remove all the monitoring services.
func (a *Admin) RemoveAllMonitoring(ignoreErrors bool) (uint16, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return 0, nil
	}

	var count uint16
	for _, svc := range node.Services {
		for _, tag := range svc.Tags {
			if !strings.HasPrefix(tag, "alias_") {
				continue
			}
			a.ServiceName = tag[6:]
			switch svc.Service {
			case plugin.LinuxMetrics:
				if err := a.RemoveMetrics(plugin.NameLinux); err != nil && !ignoreErrors {
					return count, err
				}
			case plugin.MySQLMetrics:
				if err := a.RemoveMetrics(plugin.NameMySQL); err != nil && !ignoreErrors {
					return count, err
				}
			case plugin.MySQLQueries:
				if err := a.RemoveQueries(plugin.NameMySQL); err != nil && !ignoreErrors {
					return count, err
				}
			case plugin.MongoDBMetrics:
				if err := a.RemoveMetrics(plugin.NameMongoDB); err != nil && !ignoreErrors {
					return count, err
				}
			case plugin.MongoDBQueries:
				if err := a.RemoveQueries(plugin.NameMongoDB); err != nil && !ignoreErrors {
					return count, err
				}
			case plugin.PostgreSQLMetrics:
				if err := a.RemoveMetrics(plugin.NamePostgreSQL); err != nil && !ignoreErrors {
					return count, err
				}
			case plugin.ProxySQLMetrics:
				if err := a.RemoveMetrics(plugin.NameProxySQL); err != nil && !ignoreErrors {
					return count, err
				}
			}
			count++
		}
	}

	// PMM-606: Remove generated password.
	a.Config.MySQLPassword = ""
	a.writeConfig()

	return count, nil
}

// PurgeMetrics purge metrics data on the server by its metric type and name.
func (a *Admin) PurgeMetrics(svcType string) error {
	if isValidSvcType(svcType) != nil || !strings.HasSuffix(svcType, plugin.TypeMetrics) {
		return errors.New(`bad service type.

Service type takes the following values: linux:metrics, mysql:metrics, mongodb:metrics, proxysql:metrics, postgresql:metrics.`)
	}

	var promError error

	// Delete series in Prometheus v2.
	match := fmt.Sprintf(`{job="%s",instance="%s"}`, strings.Split(svcType, ":")[0], a.ServiceName)
	url := a.qanAPI.URL(a.serverURL, fmt.Sprintf("prometheus/api/v1/admin/tsdb/delete_series?match[]=%s", match))
	resp, _, err := a.qanAPI.Post(url, []byte{})
	if err != nil || resp.StatusCode != http.StatusNoContent {
		promError = fmt.Errorf("%v:%v resp: %v", promError, err, resp)
	}

	// Clean tombstones in Prometheus v2.
	url = a.qanAPI.URL(a.serverURL, "prometheus/api/v1/admin/tsdb/clean_tombstones")
	resp, _, err = a.qanAPI.Post(url, []byte{})
	if err != nil || resp.StatusCode != http.StatusNoContent {
		promError = fmt.Errorf("%v:%v resp: %v", promError, err, resp)
	}

	return promError
}

// getConsulService get service from Consul by service type and optionally name (alias).
func (a *Admin) getConsulService(service, name string) (*consul.AgentService, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		return nil, err
	}
	for _, svc := range node.Services {
		if svc.Service != service {
			continue
		}
		if name == "" {
			return svc, nil
		}
		for _, tag := range svc.Tags {
			if tag == fmt.Sprintf("alias_%s", name) {
				return svc, nil
			}
		}
	}

	return nil, nil
}

// checkGlobalDuplicateService check if new service is globally unique and prevent duplicate clients.
func (a *Admin) checkGlobalDuplicateService(service, name string) error {
	// Prevent duplicate clients (2 or more nodes using the same name).
	// This should not usually happen unless the config file is edited manually.
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil {
		return err
	}
	if node != nil && node.Node.Address != a.Config.ClientAddress && len(node.Services) > 0 {
		return fmt.Errorf(`another client with the same name '%s' but different address detected.

This client address is %s, the other one - %s.
Re-configure this client with the different name using 'ssm-admin config' command.`,
			a.Config.ClientName, a.Config.ClientAddress, node.Node.Address)
	}

	// Check if service with the name (tag) is globally unique.
	services, _, err := a.consulAPI.Catalog().Service(service, fmt.Sprintf("alias_%s", name), nil)
	if err != nil {
		return err
	}
	if len(services) > 0 {
		return fmt.Errorf(`another client '%s' by address '%s' is monitoring %s instance under the name '%s'.

Choose different name for this service.`,
			services[0].Node, services[0].Address, service, name)
	}

	return nil
}

// checkSSLCertificate check if SSL cert and key files exist and generate them if not.
func (a *Admin) checkSSLCertificate() error {
	if FileExists(SSLCertFile) && FileExists(SSLKeyFile) {
		return nil
	}

	// Generate SSL cert and key.
	return generateSSLCertificate(a.Config.ClientAddress, SSLCertFile, SSLKeyFile)
}

// CheckVersion check server and client versions and returns boolean and error; boolean is true if error is fatal.
func (a *Admin) CheckVersion(ctx context.Context) (fatal bool, err error) {
	clientVersion, err := version.Parse(Version)
	if err != nil {
		return true, err
	}
	versionResponse, err := a.managedAPI.VersionGet(ctx)
	if err != nil {
		return true, err
	}
	serverVersion, err := version.Parse(versionResponse.Version)
	if err != nil {
		return true, err
	}

	// Return warning error if versions do not match.
	if serverVersion.Major != clientVersion.Major || clientVersion.Minor > serverVersion.Minor {
		return false, fmt.Errorf(
			"Warning: The recommended upgrade process is to upgrade SSM Server first, then SSM Clients.\n" +
				"See Shattered Silicon's instructions for upgrading at " +
				"https://shatteredsilicon.net/software/ssm/documentation/latest/deploy/",
		)
	}

	return false, nil
}

// CheckInstallation check for broken installation.
func (a *Admin) CheckInstallation() (upgradeRequired bool, orphanedServices, missingServices []string) {
	localServices := GetLocalServices()
	activeServices := GetLocalActiveServices()

	for _, svc := range localServices {
		if svc.isV1Service() {
			upgradeRequired = true
			break
		}
	}

	// check if there are new config files
	// needed migration
	for _, exporter := range exporterList {
		if _, err := os.Stat(path.Join(SSMBaseDir, "config", exporter+".conf")); err == nil {
			upgradeRequired = true
			break
		}
	}

	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		for _, svc := range activeServices {
			orphanedServices = append(orphanedServices, svc.serviceName)
		}
		return
	}

	// Find orphaned services: local system services that are not associated with Consul services.
ForLoop1:
	for _, s := range activeServices {
		for _, svc := range node.Services {
			if s.serviceType == svc.Service {
				continue ForLoop1
			}
		}
		orphanedServices = append(orphanedServices, s.serviceName)
	}

	// Find missing services: Consul services that are missing locally.
ForLoop2:
	for _, svc := range node.Services {
		for _, s := range localServices {
			if svc.Service == s.serviceType {
				continue ForLoop2
			}
		}
		missingServices = append(missingServices, svc.ID)
	}

	return upgradeRequired, orphanedServices, missingServices
}

// RepairInstallation repair installation.
func (a *Admin) RepairInstallation() error {
	upgradeRequired, orphanedServices, missingServices := a.CheckInstallation()
	if upgradeRequired {
		if err := a.Upgrade(); err != nil {
			return err
		}
		_, orphanedServices, missingServices = a.CheckInstallation()
	}

	// stop local services.
	for _, s := range orphanedServices {
		if err := stopService(s); err != nil {
			return err
		}
	}

	// Remove remote services from Consul.
	for _, s := range missingServices {
		dereg := consul.CatalogDeregistration{
			Node:      a.Config.ClientName,
			ServiceID: s,
		}
		if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
			return err
		}

		prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, s)

		// Try to delete instances from QAN associated with queries service on KV.
		names, _, err := a.consulAPI.KV().Keys(prefix, "", nil)
		if err == nil {
			for _, name := range names {
				for _, serviceName := range []string{"mysql", "mongodb"} {
					if strings.HasSuffix(name, fmt.Sprintf("/qan_%s_uuid", serviceName)) {
						data, _, err := a.consulAPI.KV().Get(name, nil)
						if err == nil && data != nil {
							a.deleteInstance(string(data.Value))
						}
						break
					}
				}
			}
		}

		a.consulAPI.KV().DeleteTree(prefix, nil)
	}

	if len(orphanedServices) > 0 || len(missingServices) > 0 {
		fmt.Printf("OK, removed %d orphaned services.\n", len(orphanedServices)+len(missingServices))
	} else {
		fmt.Println("No orphaned services found.")
	}
	return nil
}

// Uninstall remove all monitoring services with the best effort.
func (a *Admin) Uninstall() (count uint16, clientErr, serverErr error) {
	fileExists := FileExists(ConfigFile)
	if fileExists {
		err := a.LoadConfig()
		if err == nil {
			a.apiTimeout = 5 * time.Second
			if err := a.SetAPI(); err == nil {
				// Try remove all services normally ignoring the errors.
				count, _ = a.RemoveAllMonitoring(true)
			}
		}
	}

	// Find any local active SSM services and try to stop them, ignoring the errors.
	localServices := GetLocalActiveServices()

	for _, service := range localServices {
		if err := stopService(service.serviceName); err == nil {
			count++
		}
	}

	// remove saved ssm service files under /etc/systemd/system, ignore error
	exec.Command(
		"sh",
		"-c",
		"rm -f /etc/systemd/system/ssm-linux-metrics.service.rpmsave"+
			" /etc/systemd/system/ssm-linux-metrics.service.dpkg-old"+
			" /etc/systemd/system/ssm-mysql-metrics.service.rpmsave"+
			" /etc/systemd/system/ssm-mysql-metrics.service.dpkg-old"+
			" /etc/systemd/system/ssm-mongodb-metrics.service.rpmsave"+
			" /etc/systemd/system/ssm-mongodb-metrics.service.dpkg-old"+
			" /etc/systemd/system/ssm-postgresql-metrics.service.rpmsave"+
			" /etc/systemd/system/ssm-postgresql-metrics.service.dpkg-old"+
			" /etc/systemd/system/ssm-proxysql-metrics.service.rpmsave"+
			" /etc/systemd/system/ssm-proxysql-metrics.service.dpkg-old"+
			" /etc/systemd/system/ssm-mysql-queries.service.rpmsave"+
			" /etc/systemd/system/ssm-mysql-queries.service.dpkg-old"+
			" /etc/systemd/system/ssm-mongodb-queries.service.rpmsave"+
			" /etc/systemd/system/ssm-mongodb-queries.service.dpkg-old",
	).Run()

	if !fileExists {
		return
	}

	var user *url.Userinfo
	if a.Config.ServerUser != "" {
		user = url.UserPassword(a.Config.ServerUser, a.Config.ServerPassword)
	}
	schema := "http"
	if a.Config.ServerInsecureSSL || a.Config.ServerSSL {
		schema = "https"
	}
	a.managedAPI = managed.NewClient(
		a.Config.ServerAddress, a.Config.ManagedAPIPath,
		schema, user, a.Config.ServerInsecureSSL, false,
	)
	serverErr = a.managedAPI.DeleteNode(context.Background(), a.Config.ClientName)
	if serverErr != nil {
		return
	}

	// Remove agent dirs to ensure feature clean installation. Using full paths to avoid unexpected removals.
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "config"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "data"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "instance"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "trash"))

	err := a.removeConfig()
	if err != nil {
		clientErr = fmt.Errorf("remove config file %s failed: %+v", ConfigFile, err)
		return
	}

	return
}

type localService struct {
	serviceType string
	serviceName string
	filePath    string
}

func (svc localService) isPMMService() bool {
	return strings.HasPrefix(svc.serviceName, "pmm-")
}

func (svc localService) isV1Service() bool {
	return strings.Count(svc.serviceName, "-") == 3
}

func (svc localService) isQueries() bool {
	return strings.HasSuffix(svc.serviceType, ":queries")
}

func serviceTypeInName(serviceType string) string {
	return strings.Replace(serviceType, ":", "-", 1)
}

// GetLocalServices finds any local SSM/PMM services
// If v1 service files (those files with '-port' suffix) exists,
// they have higher priority
func GetLocalServices(serviceTypes ...string) (services []localService) {
	dir, extension := GetServiceDirAndExtension()

	serviceMap := make(map[string]localService)
	serviceRegex := regexp.MustCompile(`^(ssm|pmm)-([^-]+-[^-]+)(-\d+)?$`)
	walkFunc := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if path == dir {
			return nil
		}

		if d.IsDir() {
			return fs.SkipDir
		}

		if !strings.HasSuffix(d.Name(), extension) {
			return nil
		}

		name := strings.TrimSuffix(d.Name(), extension)
		parts := serviceRegex.FindStringSubmatch(name)
		if parts == nil {
			return nil
		}

		serviceType := strings.Replace(parts[2], "-", ":", 1)
		if _, ok := serviceMap[serviceType]; !ok || parts[3] != "" {
			serviceMap[serviceType] = localService{
				serviceType: serviceType,
				serviceName: name,
				filePath:    path,
			}
		}

		return nil
	}
	filepath.WalkDir(dir, walkFunc)

	if service.Platform() == systemdPlatform {
		// also get services from new systemd service dir
		dir = newSystemdDir
		filepath.WalkDir(dir, walkFunc)
	}

	for _, svc := range serviceMap {
		if len(serviceTypes) > 0 && !utils.SliceContains(serviceTypes, svc.serviceType) {
			continue
		}

		services = append(services, svc)
	}

	return services
}

// GetLocalActiveServices finds local active SSM services
func GetLocalActiveServices() (services []localService) {
	localServices := GetLocalServices()
	for _, svc := range localServices {
		if getServiceStatus(svc.serviceName) {
			services = append(services, svc)
		}
	}
	return
}

// GetServiceDirAndExtension returns dir and extension used to create system service
func GetServiceDirAndExtension() (dir, extension string) {
	switch service.Platform() {
	case systemdPlatform:
		dir = systemdDir
		extension = systemdExtension
	case upstartPlatform:
		dir = upstartDir
		extension = upstartExtension
	case systemvPlatform:
		dir = systemvDir
		extension = systemvExtension
	case launchdPlatform:
		dir = launchdDir
		extension = launchdExtension
	}

	return dir, extension
}

// ShowPasswords display passwords from config file.
func (a *Admin) ShowPasswords() {
	fmt.Println("HTTP basic authentication")
	fmt.Printf("%-8s | %s\n", "User", a.Config.ServerUser)
	fmt.Printf("%-8s | %s\n\n", "Password", a.Config.ServerPassword)

	fmt.Println("MySQL new user creation")
	fmt.Printf("%-8s | %s\n", "Password", a.Config.MySQLPassword)
	fmt.Println()
}

// FileExists check if file exists.
func FileExists(file string) bool {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return false
	}
	return true
}

// CheckBinaries check if all SSM Client binaries are at their paths
func CheckBinaries() string {
	paths := []string{
		fmt.Sprintf("%s/node_exporter", SSMBaseDir),
		fmt.Sprintf("%s/mysqld_exporter", SSMBaseDir),
		fmt.Sprintf("%s/mongodb_exporter", SSMBaseDir),
		fmt.Sprintf("%s/proxysql_exporter", SSMBaseDir),
		fmt.Sprintf("%s/postgres_exporter", SSMBaseDir),
		fmt.Sprintf("%s/bin/ssm-qan-agent", AgentBaseDir),
		fmt.Sprintf("%s/bin/ssm-qan-agent-installer", AgentBaseDir),
	}
	for _, p := range paths {
		if !FileExists(p) {
			return p
		}
	}
	return ""
}

// Output colored text.
func colorStatus(msgOK string, msgNotOK string, ok bool) string {
	c := color.New(color.FgRed, color.Bold).SprintFunc()
	if ok {
		c = color.New(color.FgGreen, color.Bold).SprintFunc()
		return c(msgOK)
	}

	return c(msgNotOK)
}

// generateSSLCertificate generate SSL certificate and key and write them into the files.
func generateSSLCertificate(host, certFile, keyFile string) error {
	// Generate key.
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %s", err)
	}

	// Generate cert.
	notBefore, _ := time.Parse("Jan 2 15:04:05 2006", "Nov 25 15:00:00 2016")
	notAfter, _ := time.Parse("Jan 2 15:04:05 2006", "Nov 25 15:00:00 2026")
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	cert := x509.Certificate{
		Subject:               pkix.Name{Organization: []string{"SSM Client"}},
		SerialNumber:          serialNumber,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		cert.IPAddresses = append(cert.IPAddresses, ip)
	} else {
		cert.DNSNames = append(cert.DNSNames, host)
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &privKey.PublicKey, privKey)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %s", err)
	}

	// Write files.
	out, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", certFile, err)
	}
	pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	out.Close()

	out, err = os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", keyFile, err)
	}
	pem.Encode(out, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})
	out.Close()

	return nil
}

var svcTypes = []string{
	plugin.LinuxMetrics,
	plugin.MySQLMetrics,
	plugin.MySQLQueries,
	plugin.MongoDBMetrics,
	plugin.MongoDBQueries,
	plugin.PostgreSQLMetrics,
	plugin.ProxySQLMetrics,
}

// isValidSvcType checks if given service type is allowed
func isValidSvcType(svcType string) error {
	for _, v := range svcTypes {
		if v == svcType {
			return nil
		}
	}

	return fmt.Errorf(`bad service type.

Service type takes the following values: %s.`, strings.Join(svcTypes, ", "))
}

func (a *Admin) remoteInstanceExists(ctx context.Context, instanceType, instanceName string) (bool, error) {
	var res *managed.RemoteListResponse
	var err error

	if instanceType == plugin.NameMySQL {
		res, err = a.managedAPI.MySQLList(ctx)
	} else if instanceType == plugin.NamePostgreSQL {
		res, err = a.managedAPI.PostgreSQLList(ctx)
	} else if instanceType == plugin.NameLinux {
		res, err = a.managedAPI.SNMPList(ctx)
	} else {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if res != nil && res.Instances != nil {
		for _, instance := range res.Instances {
			if instance.Node == nil {
				continue
			}

			if instance.Node.Name == instanceName {
				return true, nil
			}
		}
	}

	return false, nil
}

// Upgrade upgrades local services
func (a *Admin) Upgrade() (err error) {
	if err = a.migrateExporterConfigs(); err != nil {
		return err
	}

	if service.Platform() == systemdPlatform {
		if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
			return err
		}
	}

	services := GetLocalServices()
	for _, svc := range services {
		if isValidSvcType(svc.serviceType) != nil {
			continue
		}

		isRunning := getServiceStatus(svc.serviceName)
		svcName := serviceName(svc.serviceType)

		if svc.isV1Service() && !svc.isQueries() {
			switch service.Platform() {
			case systemdPlatform:
				err = a.reconfigureFromSytemd(svc)
				if err != nil {
					return err
				}
			case systemvPlatform:
				err = a.reconfigureFromSystemv(svc)
				if err != nil {
					return err
				}
			case upstartPlatform:
				err = a.reconfigureFromUpstart(svc)
				if err != nil {
					return err
				}
			}
		}

		if svc.isV1Service() {
			if err = uninstallService(svc.serviceName); err != nil {
				return err
			}
		}

		if !isRunning {
			continue
		}

		if err = restartService(svcName); err != nil {
			return err
		}
	}

	return nil
}

func (a *Admin) reconfigureFromSytemd(svc localService) error {
	upgradeSvcName := upgradeServiceName(svc.serviceType)
	upgradeSvcFilePath := path.Join(systemdDir, upgradeSvcName+systemdExtension)

	err := exec.Command("cp", svc.filePath, upgradeSvcFilePath).Run()
	if err != nil {
		return err
	}

	unitFile, err := ini.ShadowLoad(upgradeSvcFilePath)
	if err != nil {
		return err
	}

	if !unitFile.HasSection("Service") {
		return nil
	}

	// the ini.ShadowLoad method removes the double quotes around
	// environemnt variables, we need those double quotes back.
	envStrs := unitFile.Section("Service").Key("Environment").ValueWithShadows()
	unitFile.Section("Service").DeleteKey("Environment")
	for _, envStr := range envStrs {
		unitFile.Section("Service").Key("Environment").AddShadow(strconv.Quote(envStr))
	}

	err = unitFile.Section("Service").Key("Environment").AddShadow("ON_CONFIGURE=1")
	if err != nil {
		return err
	}

	unitFile.Section("Service").Key("Restart").SetValue("no")
	if err = unitFile.SaveTo(upgradeSvcFilePath); err != nil {
		return err
	}

	if err = a.replacePMMDir(svc, upgradeSvcFilePath); err != nil {
		return err
	}

	err = restartService(upgradeSvcName)
	if err != nil {
		return err
	}

	for i := 0; i < 100; i++ {
		time.Sleep(50 * time.Millisecond)
		if !getServiceStatus(upgradeSvcName) {
			break
		}
	}

	if err = uninstallService(upgradeSvcName); err != nil {
		return err
	}

	return nil
}

func (a *Admin) reconfigureFromSystemv(svc localService) error {
	upgradeSvcName := upgradeServiceName(svc.serviceType)
	upgradeSvcFilePath := path.Join(systemvDir, upgradeSvcName+systemvExtension)

	err := exec.Command("cp", svc.filePath, upgradeSvcFilePath).Run()
	if err != nil {
		return err
	}

	err = exec.Command("sed", "-i", "1 i export ON_CONFIGURE=1", upgradeSvcFilePath).Run()
	if err != nil {
		return err
	}

	if err = a.replacePMMDir(svc, upgradeSvcFilePath); err != nil {
		return err
	}

	err = restartService(upgradeSvcName)
	if err != nil {
		return err
	}

	for i := 0; i < 100; i++ {
		time.Sleep(50 * time.Millisecond)
		if !getServiceStatus(upgradeSvcName) {
			break
		}
	}

	if err = uninstallService(upgradeSvcName); err != nil {
		return err
	}

	return nil
}

func (a *Admin) reconfigureFromUpstart(svc localService) error {
	upgradeSvcName := upgradeServiceName(svc.serviceType)
	upgradeSvcFilePath := path.Join(upstartDir, upgradeSvcName+upstartExtension)

	err := exec.Command("cp", svc.filePath, upgradeSvcFilePath).Run()
	if err != nil {
		return err
	}

	err = exec.Command("sed", "-i", "1 i env ON_CONFIGURE=1", upgradeSvcFilePath).Run()
	if err != nil {
		return err
	}

	err = exec.Command("sed", "-i", "s/\\(start[[:space:]]+on[[:space:]]+stopped\\)/# \\1/g", upgradeSvcFilePath).Run()
	if err != nil {
		return err
	}

	if err = a.replacePMMDir(svc, upgradeSvcFilePath); err != nil {
		return err
	}

	err = restartService(upgradeSvcName)
	if err != nil {
		return err
	}

	for i := 0; i < 100; i++ {
		time.Sleep(50 * time.Millisecond)
		if !getServiceStatus(upgradeSvcName) {
			break
		}
	}

	if err = uninstallService(upgradeSvcName); err != nil {
		return err
	}

	return nil
}

func (a *Admin) replacePMMDir(svc localService, serviceFilePath string) error {
	if !svc.isPMMService() {
		return nil
	}
	return exec.Command(
		"sed",
		"-i",
		fmt.Sprintf("s/%s/%s/g",
			strings.Replace(PMMBaseDir, "/", "\\/", -1),
			strings.Replace(SSMBaseDir, "/", "\\/", -1),
		),
		serviceFilePath,
	).Run()
}

func (a *Admin) migrateExporterConfigs() error {
	configsDir := path.Join(SSMBaseDir, "config")

	for _, exporter := range exporterList {
		newConfigPath := path.Join(configsDir, exporter+".conf")
		oriConfigPath := path.Join(SSMBaseDir, exporter+".conf")

		// new config
		newStat, err := os.Stat(newConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		// original config
		oriStat, err := os.Stat(oriConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		// just mv new config if original config doesn't exist
		if newStat != nil && oriStat == nil {
			if err = os.Rename(newConfigPath, oriConfigPath); err != nil {
				return err
			}
			continue
		}

		if oriStat != nil && oriStat.Mode().String() != "-rw-------" {
			if err = os.Chmod(oriConfigPath, 0600); err != nil {
				return err
			}
		}

		// following merge process won't be necessary if
		// one of new/ori configs doesn't exists
		if newStat == nil || oriStat == nil {
			continue
		}

		// merge new config into original config
		newIni, err := ini.Load(newConfigPath)
		if err != nil {
			return err
		}
		oriIni, err := ini.Load(oriConfigPath)
		if err != nil {
			return err
		}

		for _, sectionName := range append(newIni.SectionStrings(), "") {
			for _, keyName := range newIni.Section(sectionName).KeyStrings() {
				if oriIni.Section(sectionName).HasKey(keyName) {
					continue
				}

				_, err = oriIni.Section(sectionName).NewKey(keyName, newIni.Section(sectionName).Key(keyName).Value())
				if err != nil {
					return err
				}
			}
		}

		if err = oriIni.SaveTo(oriConfigPath); err != nil {
			return err
		}

		if err = os.Remove(newConfigPath); err != nil {
			return err
		}
	}

	// delete the config dir, and if there's a problem
	// deleteing the config dir (either it's not empty,
	// not exists or it just fails to delete it), we just
	// ignore it as all the config file have been migrated,
	// we can live with this dir not being removed
	os.Remove(configsDir)
	return nil
}
