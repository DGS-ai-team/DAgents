Name:           dagents-backend
Version:        @VERSION@
Release:        1%{?dist}
Summary:        DAgents backend runtime
License:        MIT
URL:            https://github.com/DAgents/DAgents
BuildArch:      @BUILDARCH@
AutoReqProv:    no

%description
Installs the DAgents backend runtime under /opt/dagents/backend and exposes
the dagents command in /usr/bin for chat, serve, and register-center.

%prep

%build

%install
rm -rf %{buildroot}
mkdir -p %{buildroot}/opt/dagents/backend %{buildroot}/usr/bin
cp -a %{_sourcedir}/dagents-backend-%{version}/bundle/. %{buildroot}/opt/dagents/backend/
install -m 0755 %{_sourcedir}/dagents-backend-%{version}/dagents %{buildroot}/usr/bin/dagents
chmod 0755 %{buildroot}/opt/dagents/backend/dagents-api %{buildroot}/opt/dagents/backend/dagents_register_center %{buildroot}/opt/dagents/backend/dagents-cli 2>/dev/null || true

%files
/usr/bin/dagents
/opt/dagents/backend

%post
chmod 0755 /usr/bin/dagents || true
chmod 0755 /opt/dagents/backend/dagents-api /opt/dagents/backend/dagents_register_center /opt/dagents/backend/dagents-cli || true

%postun
