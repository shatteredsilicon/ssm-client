%define debug_package %{nil}

%define _GOPATH %{_builddir}/go

Name:           ssm-client
Summary:        Shattered Silicon Monitoring Client
Version:        %{_version}
Release:        %{_release}
Group:          Applications/Databases
License:        AGPLv3
Vendor:         Shattered Silicon
URL:            https://shatteredsilicon.net
Source0:        ssm-client-%{version}-%{release}.tar.gz
AutoReq:        no
BuildRequires:  glibc-devel, glibc-static, golang >= 1.24, unzip, gzip, make, perl-ExtUtils-MakeMaker, git, systemd

Obsoletes: pmm-client <= 1.17.5
Conflicts: pmm-client > 1.17.5

Requires: percona-toolkit

Requires(post):     systemd
Requires(preun):    systemd
Requires(postun):   systemd

%description
Shattered Silicon Monitoring (SSM) is an open-source platform for managing and monitoring MySQL and MongoDB
performance. It is a fork of Percona Monitoring and Management (PMM), which is developed by Percona in collaboration
with experts in the field of managed database services, and further improved by Shattered Silicon.
SSM is a free and open-source solution that you can run in your own environment for maximum security and reliability.
It provides thorough time-based analysis for MySQL and MongoDB servers to ensure that your data works as efficiently
as possible.

%prep
%setup -q -n %{name}

%build
mkdir -p %{_GOPATH}

export GOPATH=%{_GOPATH}
export GO111MODULE=off
export CGO_ENABLED=0

%{__mkdir_p} %{_GOPATH}/src/github.com/prometheus
%{__mkdir_p} %{_GOPATH}/src/github.com/shatteredsilicon
%{__mkdir_p} %{_GOPATH}/bin

mv -fT submodules/qan-agent %{_GOPATH}/src/github.com/shatteredsilicon/qan-agent
mv -fT submodules/node_exporter %{_GOPATH}/src/github.com/shatteredsilicon/node_exporter
mv -fT submodules/mysqld_exporter %{_GOPATH}/src/github.com/shatteredsilicon/mysqld_exporter
mv -fT submodules/mongodb_exporter %{_GOPATH}/src/github.com/shatteredsilicon/mongodb_exporter
mv -fT submodules/postgres_exporter %{_GOPATH}/src/github.com/shatteredsilicon/postgres_exporter
mv -fT submodules/proxysql_exporter %{_GOPATH}/src/github.com/shatteredsilicon/proxysql_exporter
mv -fT submodules/mt-agent %{_GOPATH}/src/github.com/shatteredsilicon/mt-agent

go install -ldflags="-s -w" github.com/shatteredsilicon/node_exporter
go install -ldflags="-s -w" github.com/shatteredsilicon/postgres_exporter/cmd/postgres_exporter
go install -ldflags="-s -w" github.com/shatteredsilicon/mongodb_exporter
go install -ldflags="-s -w" github.com/shatteredsilicon/proxysql_exporter
go install -ldflags="-s -w" github.com/shatteredsilicon/mysqld_exporter
go install -ldflags="-s -w -X 'main.DefaultTuningFile=/etc/my.cnf.d/conf.d/zz-ssm-tuning.cnf'" github.com/shatteredsilicon/mt-agent/cmd/mt-agent
pushd %{_GOPATH}/src/github.com/shatteredsilicon/qan-agent
    CGO_ENABLED=1 GO111MODULE=on go install -mod=vendor -buildvcs=false -tags netgo,osusergo -ldflags="-s -w -linkmode 'external' -extldflags '-static'" ./bin/...
popd
GO111MODULE=on go install -mod=vendor -ldflags="-s -w -X 'github.com/shatteredsilicon/ssm-client/ssm.Version=%{version}-%{release}'" .

strip %{_GOPATH}/bin/* || true

%install
install -m 0755 -d $RPM_BUILD_ROOT/usr/sbin
install -m 0755 %{_GOPATH}/bin/ssm-client $RPM_BUILD_ROOT/usr/sbin/ssm-admin
install -m 0755 %{_GOPATH}/bin/ssm-client $RPM_BUILD_ROOT/usr/sbin/pmm-admin
install -m 0755 -d $RPM_BUILD_ROOT/opt/ss/ssm-client
install -m 0755 -d $RPM_BUILD_ROOT/opt/ss/qan-agent/bin
install -m 0755 -d $RPM_BUILD_ROOT/opt/ss/ssm-client/textfile-collector
install -m 0755 -d $RPM_BUILD_ROOT/lib/systemd/system
install -m 0755 -d $RPM_BUILD_ROOT/etc/rsyslog.d/
install -m 0755 -d $RPM_BUILD_ROOT/etc/logrotate.d/
install -m 0755 %{_GOPATH}/bin/node_exporter $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0755 %{_GOPATH}/bin/mysqld_exporter $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0755 %{_GOPATH}/bin/postgres_exporter $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0755 %{_GOPATH}/bin/mongodb_exporter $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0755 %{_GOPATH}/bin/proxysql_exporter $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0755 %{_GOPATH}/bin/ssm-qan-agent $RPM_BUILD_ROOT/opt/ss/qan-agent/bin/
install -m 0755 %{_GOPATH}/bin/ssm-qan-agent-installer $RPM_BUILD_ROOT/opt/ss/qan-agent/bin/
install -m 0755 %{_GOPATH}/bin/mt-agent $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0644 %{_GOPATH}/src/github.com/shatteredsilicon/mysqld_exporter/queries-mysqld.yml $RPM_BUILD_ROOT/opt/ss/ssm-client
install -m 0755 %{_GOPATH}/src/github.com/shatteredsilicon/node_exporter/example.prom $RPM_BUILD_ROOT/opt/ss/ssm-client/textfile-collector/
install -m 0600 %{_GOPATH}/src/github.com/shatteredsilicon/{node_exporter,mysqld_exporter,mongodb_exporter,postgres_exporter,proxysql_exporter,mt-agent}/support-files/config/*.conf $RPM_BUILD_ROOT/opt/ss/ssm-client/
install -m 0644 %{_GOPATH}/src/github.com/shatteredsilicon/{node_exporter,mysqld_exporter,mongodb_exporter,postgres_exporter,proxysql_exporter,qan-agent}/ssm-*.service $RPM_BUILD_ROOT/lib/systemd/system/
install -m 0644 %{_GOPATH}/src/github.com/shatteredsilicon/mt-agent/support-files/systemd/ssm-*.service $RPM_BUILD_ROOT/lib/systemd/system/
install -m 0644 %{_GOPATH}/src/github.com/shatteredsilicon/{node_exporter,mysqld_exporter,mongodb_exporter,postgres_exporter,proxysql_exporter,qan-agent,mt-agent}/support-files/rsyslog.d/* $RPM_BUILD_ROOT/etc/rsyslog.d/
install -m 0644 %{_GOPATH}/src/github.com/shatteredsilicon/{node_exporter,mysqld_exporter,mongodb_exporter,postgres_exporter,proxysql_exporter,qan-agent,mt-agent}/support-files/logrotate.d/* $RPM_BUILD_ROOT/etc/logrotate.d/

%clean
rm -rf $RPM_BUILD_ROOT

%pre
if [ $1 -gt 1 ]; then
    # save old version, so we can know what's the last version that
    # it upgraded from later
    if command -v ssm-admin 2>&1 >/dev/null; then
        ssm-admin --version > /opt/ss/ssm-client/.old-version
    fi
fi

%post
# Upgrade
if [ $1 -gt 1 ] || [ -f /usr/local/percona/pmm-client/pmm.yml ]; then
    # Upgrade from PMM
    if [ -f /usr/local/percona/pmm-client/pmm.yml ]; then
        cp -n /usr/local/percona/pmm-client/pmm.yml /opt/ss/ssm-client/ssm.yml
    fi
    if [ -f /usr/local/percona/pmm-client/server.crt ]; then
        cp -n /usr/local/percona/pmm-client/server.crt /opt/ss/ssm-client/server.crt
    fi
    if [ -f /usr/local/percona/pmm-client/server.key ]; then
        cp -n /usr/local/percona/pmm-client/server.key /opt/ss/ssm-client/server.key
    fi
    if [ -d /usr/local/percona/qan-agent ] && [ ! -f /opt/ss/qan-agent/config/agent.conf ]; then
        find /usr/local/percona/qan-agent -maxdepth 1 ! -path /usr/local/percona/qan-agent ! -name bin -exec cp -r "{}" /opt/ss/qan-agent/ \;
    fi

    # from 9.x going forward, we leave the .rpmnew config files alone
    if ! [ -f /opt/ss/ssm-client/.old-version ] || [[ "$(cat /opt/ss/ssm-client/.old-version)" =~ ^[vV]?[0-8][.] ]]; then
        # move new exporter config files to /opt/ss/ssm-client/config/,
        # so we can handle config migration in same way on deb and rpm
        for file in /opt/ss/ssm-client/*.conf.rpmnew; do
            if ! [ -f "$file" ]; then
                continue
            fi

            if ! [ -d /opt/ss/ssm-client/config ]; then
                mkdir /opt/ss/ssm-client/config
            fi

            trim_file="${file%.rpmnew}"
            mv "$file" "/opt/ss/ssm-client/config/${trim_file##*/}"
        done
    fi

    # backup ssm service files under /etc/systemd/system
    for file in /etc/systemd/system/ssm-{linux,mysql,mongodb,postgresql,proxysql}-metrics.service /etc/systemd/system/ssm-{mysql,mongodb}-queries.service; do
        if ! [ -f "$file" ]; then
            continue
        fi

        mv "$file" "${file}.rpmsave"
    done

    # `ssm-admin upgrade` runs `systemctl daemon-reload`
    ssm-admin upgrade

    # copy back ssm service file to /etc/systemd/system because
    # they are listed in old package's %files section
    for file in /etc/systemd/system/ssm-{linux,mysql,mongodb,postgresql,proxysql}-metrics.service.rpmsave /etc/systemd/system/ssm-{mysql,mongodb}-queries.service.rpmsave; do
        if ! [ -f "$file" ]; then
            continue
        fi

        cp "$file" "${file%.rpmsave}"
    done
fi

%systemd_post ssm-linux-metrics.service
%systemd_post ssm-mysql-metrics.service
%systemd_post ssm-mysql-queries.service
%systemd_post ssm-mysql-tuning.service
%systemd_post ssm-mongodb-metrics.service
%systemd_post ssm-mongodb-queries.service
%systemd_post ssm-postgresql-metrics.service
%systemd_post ssm-proxysql-metrics.service

%preun
# uninstall
if [ "$1" = "0" ]; then
    ssm-admin uninstall
fi

%systemd_preun ssm-linux-metrics.service
%systemd_preun ssm-mysql-metrics.service
%systemd_preun ssm-mysql-queries.service
%systemd_preun ssm-mysql-tuning.service
%systemd_preun ssm-mongodb-metrics.service
%systemd_preun ssm-mongodb-queries.service
%systemd_preun ssm-postgresql-metrics.service
%systemd_preun ssm-proxysql-metrics.service

%postun
# uninstall
if [ "$1" = "0" ]; then
    rm -rf /opt/ss/ssm-client
    rm -rf /opt/ss/qan-agent
    rm -f /etc/systemd/system/ssm-{linux,mysql,mongodb,postgresql,proxysql}-metrics.service.rpmsave
    rm -f /etc/systemd/system/ssm-{mysql,mongodb}-queries.service.rpmsave
    rm -f /etc/systemd/system/ssm-mysql-tuning.service.rpmsave
    echo "Uninstall complete."
fi

%systemd_postun ssm-linux-metrics.service
%systemd_postun ssm-mysql-metrics.service
%systemd_postun ssm-mysql-queries.service
%systemd_postun ssm-mysql-tuning.service
%systemd_postun ssm-mongodb-metrics.service
%systemd_postun ssm-mongodb-queries.service
%systemd_postun ssm-postgresql-metrics.service
%systemd_postun ssm-proxysql-metrics.service

%files
%dir /opt/ss/ssm-client
%dir /opt/ss/ssm-client/textfile-collector
%dir /opt/ss/qan-agent/bin
/opt/ss/ssm-client/textfile-collector/*
/opt/ss/ssm-client/*
%config(noreplace) /opt/ss/ssm-client/*.conf
/opt/ss/qan-agent/bin/*
%config /lib/systemd/system/ssm-*.service
/usr/sbin/ssm-admin
/usr/sbin/pmm-admin
%config(noreplace) /etc/rsyslog.d/ssm-*.conf
%config(noreplace) /etc/logrotate.d/ssm-*
