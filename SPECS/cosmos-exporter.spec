Name:         cosmos-exporter
Version:      1.0
Release:      %{_releasetag}%{?dist}
Summary:      Cosmos Monitoring Data Exporter

License:      GPL3
URL:          https://github.com/wajones98/cosmos-exporter           

Source0:      cosmos-exporter-1.0.tar.gz

BuildRequires: golang

# undefine __brp_mangle_shebangs

%description
Monitoring data exporter for the cosmos and parts of the ethereum blockchains

%prep
echo -e "\n\n=== prep section ===\n\n"
# Unpack tarball

BASEDR="$( pwd )"
tar xzvf %{SOURCE0}

%build
echo -e "\n\n=== build section ===\n\n"

export GOPATH="${RPM_BUILD_DIR}/go"

echo -e "\n\n=== Build and install cosmos-exporter ===\n\n"

go build -v .

%install
echo -e "\n\n=== install section ===\n\n"

# Make the fixed directory structure
mkdir -p ${RPM_BUILD_ROOT}/var/lib/cosmos
mkdir -p ${RPM_BUILD_ROOT}/usr/bin/
mkdir -p ${RPM_BUILD_ROOT}/usr/lib/systemd/system

# Copy the newly built binaries into /usr/bin and /lib64
cp -v ${RPM_BUILD_DIR}/go/bin/cosmos-exporter          ${RPM_BUILD_ROOT}/usr/bin/

# Install the config files
cp -rv ${RPM_SOURCE_DIR}/dashboards                    ${RPM_BUILD_ROOT}/var/lib/cosmos/
cp -rv ${RPM_SOURCE_DIR}/images                        ${RPM_BUILD_ROOT}/var/lib/cosmos/

# Install systemd service file
cp ${RPM_SOURCE_DIR}/*.service                         ${RPM_BUILD_ROOT}/usr/lib/systemd/system/

%clean
# rm -rf $RPM_BUILD_ROOT

%pre
getent group cosmos >/dev/null || groupadd -r cudos || :
getent passwd cosmos >/dev/null || useradd -c "Cosmos Exporter User" -g cosmos -s /bin/bash -r -m -d /var/lib/cosmos cosmos 2> /dev/null || :

%post
if [ $1 = "1" ]
then
    echo "Install .. but no scripts today"
else
    echo "Upgrade .. still no scripts today"
fi

%files
%defattr(-,root,root,-)
/usr/bin/cosmos-exporter
/usr/lib/systemd/system/*
%doc
%defattr(-,cudos,cudos,-)
%config(noreplace) /var/lib/cosmos/*

%changelog

