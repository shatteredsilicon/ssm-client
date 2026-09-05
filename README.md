# Shattered Silicon Monitoring (SSM) Client

[![Go Report Card](https://goreportcard.com/badge/github.com/shatteredsilicon/ssm-client)](https://goreportcard.com/report/github.com/shatteredsilicon/ssm-client)

See the [SSM docs](https://shatteredsilicon.net/software/ssm/documentation/latest/) for more information.

## Setting up the CentOS 6 environment with Docker and build

Use Docker to setting up a CentOS 6 environemnt with following command:

```
docker run -it -d --privileged library/centos:6.10 bash
```

And run following commands inside the container:

```
rm -f /etc/yum.repos.d/*.repo

cat << 'EOF' > /etc/yum.repos.d/CentOS-Base.repo
[base]
name=CentOS-6.10 - Base Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/os/x86_64/
gpgcheck=1
gpgkey=https://archive.kernel.org/centos-vault/RPM-GPG-KEY-CentOS-6
enabled=1

[updates]
name=CentOS-6.10 - Updates Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/updates/x86_64/
gpgcheck=1
gpgkey=https://archive.kernel.org/centos-vault/RPM-GPG-KEY-CentOS-6
enabled=1

[extras]
name=CentOS-6.10 - Extras Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/extras/x86_64/
gpgcheck=1
gpgkey=https://archive.kernel.org/centos-vault/RPM-GPG-KEY-CentOS-6
enabled=1

[sclo-rh]
name=CentOS-6.10 - SCLO Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/sclo/x86_64/rh
gpgcheck=0
enabled=1

[go-el$releasever-$basearch]
name=go-el$releasever-$basearch
baseurl=https://dl.shatteredsilicon.net/misc/6/RPMS/$basearch
gpgkey=https://dl.shatteredsilicon.net/misc/RPM-GPG-KEY-SS-MISC
gpgcheck=1
enabled=1
priority=999
EOF

yum install -y git tar rpmdevtools epel-release golang-1.23.12
yum install -y mock

cat << 'EOF' > /etc/mock/centos-6-x86_64.cfg
config_opts['root'] = 'centos-6-x86_64'
config_opts['target_arch'] = 'x86_64'
config_opts['legal_host_arches'] = ('x86_64',)

config_opts['package_manager'] = 'yum'
config_opts['use_bootstrap'] = False
config_opts['environment']['OPENSSL_ENABLE_SHA1_SIGNATURES'] = '1'

config_opts['chroot_setup_cmd'] = 'install bash bzip2 coreutils cpio diffutils findutils gawk grep gzip info make patch sed shadow-utils tar unzip xz yum rpmdevtools'
config_opts['dist'] = 'el6'
config_opts['releasever'] = '6'
config_opts['dnf_warning'] = False

config_opts['yum.conf'] = """
[main]
assumeyes=1
reposdir=/dev/null

[base]
name=CentOS-6.10 - Base Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/os/x86_64/
gpgcheck=1
gpgkey=https://archive.kernel.org/centos-vault/RPM-GPG-KEY-CentOS-6
enabled=1

[updates]
name=CentOS-6.10 - Updates Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/updates/x86_64/
gpgcheck=1
gpgkey=https://archive.kernel.org/centos-vault/RPM-GPG-KEY-CentOS-6
enabled=1

[extras]
name=CentOS-6.10 - Extras Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/extras/x86_64/
gpgcheck=1
gpgkey=https://archive.kernel.org/centos-vault/RPM-GPG-KEY-CentOS-6
enabled=1

[sclo-rh]
name=CentOS-6.10 - SCLO Archive
baseurl=https://archive.kernel.org/centos-vault/6.10/sclo/x86_64/rh
gpgcheck=0
enabled=1

[go-el$releasever-$basearch]
name=go-el$releasever-$basearch
baseurl=https://dl.shatteredsilicon.net/misc/6/RPMS/$basearch
gpgkey=https://dl.shatteredsilicon.net/misc/RPM-GPG-KEY-SS-MISC
gpgcheck=1
enabled=1
priority=999
"""
EOF

git clone -b el6 https://github.com/shatteredsilicon/ssm-client.git ~/ssm-client && cd ~/ssm-client
BUILDDIR=~/rpmbuild VERSION=9.4.21 make
```

And finally the ssm-client package should be located in `~/rpmbuild/results/RPMS/ssm-client-9.4.21-1.el6.x86_64.rpm` if all these preceding commands were executed successfully.