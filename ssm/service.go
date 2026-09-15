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
	"fmt"
	"os/exec"

	service "github.com/percona/kardianos-service"
)

var (
	NewService func(i service.Interface, c *service.Config) (service.Service, error) = service.New
)

// @todo don't use singleton init, use dependency injection
func init() {
	// if we build app in tests then let's mock service installer
	if Version == "gotest" {
		NewService = func(i service.Interface, c *service.Config) (service.Service, error) {
			return &dummyService{}, nil
		}
	}

	for _, sys := range service.AvailableSystems() {
		if sys.String() == "unix-systemv" || sys.String() == "sysv" {
			service.ChooseSystem(sys)
			break
		}
	}
}

type dummyService struct {
}

func (*dummyService) Run() error       { return nil }
func (*dummyService) Start() error     { return nil }
func (*dummyService) Stop() error      { return nil }
func (*dummyService) Restart() error   { return nil }
func (*dummyService) Install() error   { return nil }
func (*dummyService) Uninstall() error { return nil }
func (*dummyService) Status() error    { return nil }
func (*dummyService) Logger(errs chan<- error) (service.Logger, error) {
	return service.ConsoleLogger, nil
}
func (*dummyService) SystemLogger(errs chan<- error) (service.Logger, error) {
	return service.ConsoleLogger, nil
}
func (*dummyService) String() string { return "" }

// Platform service manager handlers.
type program struct{}

func (p *program) Start(s service.Service) error {
	return nil
}

func (p *program) Stop(s service.Service) error {
	return nil
}

func (p *program) run() error {
	return nil
}

func installService(svcConfig *service.Config) error {
	prg := &program{}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Install(); err != nil {
		return err
	}
	if err := svc.Start(); err != nil {
		return err
	}
	return nil
}

func uninstallService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Status(); err == nil {
		if err := svc.Stop(); err != nil {
			return err
		}
	}
	if err := svc.Uninstall(); err != nil {
		return err
	}
	return nil
}

func startService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Status(); err != nil {
		if err := svc.Start(); err != nil {
			return err
		}
	}
	return nil
}

func stopService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Status(); err == nil {
		if err := svc.Stop(); err != nil {
			return err
		}
	}
	return nil
}

func restartService(name string) error {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return err
	}
	if err := svc.Restart(); err != nil {
		return err
	}
	return nil
}

func enableService(name string) error {
	switch service.Platform() {
	case systemdPlatform:
		return exec.Command("systemctl", "enable", name).Run()
	case systemvPlatform:
		return exec.Command("chkconfig", name, "on").Run()
	default:
		return nil
	}
}

func disableService(name string) error {
	switch service.Platform() {
	case systemdPlatform:
		return exec.Command("systemctl", "disable", name).Run()
	case systemvPlatform:
		return exec.Command("chkconfig", name, "off").Run()
	default:
		return nil
	}
}

func getServiceStatus(name string) bool {
	prg := &program{}
	svcConfig := &service.Config{Name: name}
	svc, err := NewService(prg, svcConfig)
	if err != nil {
		return false
	}
	if err := svc.Status(); err != nil {
		return false
	}
	return true
}

var (
	systemdDir    = RootDir + "/etc/systemd/system"
	newSystemdDir = RootDir + "/lib/systemd/system"
	upstartDir    = RootDir + "/etc/init"
	systemvDir    = RootDir + "/etc/init.d"
	launchdDir    = RootDir + "/Library/LaunchDaemons"
)

const (
	systemdPlatform = "linux-systemd"
	upstartPlatform = "linux-upstart"
	systemvPlatform = "unix-systemv"
	launchdPlatform = "darwin-launchd"

	systemdExtension = ".service"
	upstartExtension = ".conf"
	systemvExtension = ""
	launchdExtension = ".plist"

	upgradeServiceSuffix = "-upgrade"
)

func serviceName(serviceType string) string {
	if isValidSvcType(serviceType) != nil {
		return ""
	}

	return fmt.Sprintf("ssm-%s", serviceTypeInName(serviceType))
}

func upgradeServiceName(serviceType string) string {
	if name := serviceName(serviceType); name != "" {
		return fmt.Sprintf("%s%s", name, upgradeServiceSuffix)
	}

	return ""
}
