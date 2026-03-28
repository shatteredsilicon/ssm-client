BUILDDIR	?= /tmp/ssmbuild
VERSION		?=
RELEASE		?= 1

.PHONY: all
all:

ifeq (0, $(shell hash dpkg 2>/dev/null; echo $$?))
ARCH	:= $(shell dpkg --print-architecture)
all: sdeb deb
else
ARCH	:= $(shell rpm --eval "%{_arch}")
all: srpm rpm
endif

TARBALL_FILE	:= $(BUILDDIR)/tarballs/ssm-client-$(VERSION)-$(RELEASE).tar.gz
SRPM_FILE		:= $(BUILDDIR)/results/SRPMS/ssm-client-$(VERSION)-$(RELEASE).src.rpm
RPM_FILE		:= $(BUILDDIR)/results/RPMS/ssm-client-$(VERSION)-$(RELEASE).$(ARCH).rpm
SDEB_FILES		:= $(BUILDDIR)/results/SDEBS/ssm-client_$(VERSION)-$(RELEASE).dsc $(BUILDDIR)/results/SDEBS/ssm-client_$(VERSION)-$(RELEASE).tar.gz
DEB_FILES		:= $(BUILDDIR)/results/DEBS/ssm-client_$(VERSION)-$(RELEASE)_$(ARCH).deb $(BUILDDIR)/results/DEBS/ssm-client_$(VERSION)-$(RELEASE)_$(ARCH).changes

$(TARBALL_FILE):
	mkdir -vp $(shell dirname $(TARBALL_FILE))

	GOTOOLCHAIN=auto GO111MODULE=on go mod vendor
	git submodule update --init --force

	for submodule_dir in $(shell find $(CURDIR)/submodules -maxdepth 1 -mindepth 1 -type d); do \
		cd $${submodule_dir}; \
			GOTOOLCHAIN=auto GO111MODULE=on go mod vendor || exit 1; \
	done; \

	tar --exclude-vcs -czf $(TARBALL_FILE) -C $(shell dirname $(CURDIR)) --transform s/^$(shell basename $(CURDIR))/ssm-client/ $(shell basename $(CURDIR))

.PHONY: srpm
srpm: $(SRPM_FILE)

$(SRPM_FILE): $(TARBALL_FILE)
	mkdir -vp $(BUILDDIR)/rpmbuild/{SOURCES,SPECS,BUILD,SRPMS,RPMS}
	mkdir -vp $(shell dirname $(SRPM_FILE))

	cp ssm-client.spec $(BUILDDIR)/rpmbuild/SPECS/ssm-client.spec
	sed -i "s/%{_version}/$(VERSION)/g" "$(BUILDDIR)/rpmbuild/SPECS/ssm-client.spec"
	sed -i "s/%{_release}/$(RELEASE)/g" "$(BUILDDIR)/rpmbuild/SPECS/ssm-client.spec"
	cp $(TARBALL_FILE) $(BUILDDIR)/rpmbuild/SOURCES/
	rpmbuild -bs --define "debug_package %{nil}" --define "_topdir $(BUILDDIR)/rpmbuild" $(BUILDDIR)/rpmbuild/SPECS/ssm-client.spec
	mv $(BUILDDIR)/rpmbuild/SRPMS/$(shell basename $(SRPM_FILE)) $(SRPM_FILE)

.PHONY: rpm
rpm: $(RPM_FILE)

$(RPM_FILE): $(SRPM_FILE)
	mkdir -vp $(BUILDDIR)/mock $(shell dirname $(RPM_FILE))
	mock -r ssm-7-$$(rpm --eval "%{_arch}") --resultdir $(BUILDDIR)/mock --rebuild $(SRPM_FILE)
	mv $(BUILDDIR)/mock/$(shell basename $(RPM_FILE)) $(RPM_FILE)

.PHONY: sdeb
sdeb: $(SDEB_FILES)

$(SDEB_FILES): $(TARBALL_FILE)
	mkdir -vp $(BUILDDIR)/debbuild/SDEB/ssm-client-$(VERSION)-$(RELEASE)
	cp -r debian $(TARBALL_FILE) $(BUILDDIR)/debbuild/SDEB/ssm-client-$(VERSION)-$(RELEASE)/

	cd $(BUILDDIR)/debbuild/SDEB/ssm-client-$(VERSION)-$(RELEASE)/; \
		sed -i "s/%{_version}/$(VERSION)/g"  debian/control; \
		sed -i "s/%{_release}/$(RELEASE)/g"  debian/control; \
		sed -i "s/%{_version}/$(VERSION)/g"  debian/rules; \
		sed -i "s/%{_release}/$(RELEASE)/g"  debian/rules; \
		sed -i "s/%{_version}/$(VERSION)/g"  debian/changelog; \
		sed -i "s/%{_release}/$(RELEASE)/g"  debian/changelog; \
		dpkg-buildpackage -S -us

	for sdeb_file in $(SDEB_FILES); do \
		mkdir -vp $$(dirname $${sdeb_file}); \
		mv -f $(BUILDDIR)/debbuild/SDEB/$$(basename $${sdeb_file}) $${sdeb_file}; \
	done

.PHONY: deb
deb: $(DEB_FILES)

$(DEB_FILES): $(SDEB_FILES)
	mkdir -vp $(BUILDDIR)/debbuild/DEB/ssm-client-$(VERSION)-$(RELEASE)
	for sdeb_file in $(SDEB_FILES); do \
		cp -r $${sdeb_file} $(BUILDDIR)/debbuild/DEB/ssm-client-$(VERSION)-$(RELEASE)/; \
	done

	cd $(BUILDDIR)/debbuild/DEB/ssm-client-$(VERSION)-$(RELEASE)/; \
		rm -rf ssm-client-$(VERSION); \
		dpkg-source -x -sp ssm-client_$(VERSION)-$(RELEASE).dsc; \
		cd ssm-client-$(VERSION); \
			dpkg-buildpackage -b -uc

	for deb_file in $(DEB_FILES); do \
		mkdir -vp $$(dirname $${deb_file}); \
		mv -f $(BUILDDIR)/debbuild/DEB/ssm-client-$(VERSION)-$(RELEASE)/$$(basename $${deb_file}) $${deb_file}; \
	done

.PHONY: clean
clean:
	rm -rf $(BUILDDIR)/{tarballs,rpmbuild,debbuild,mock,results}