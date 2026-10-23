# Windows installer

## Installer contents

The Inno Setup package installs the Agent Node, command-line tools, and the
standalone Tauri Desktop application. The installer has one supported Desktop
Shell; the former Go browser-based Shell has been retired and is not included
in bundles or offered as an installation choice.

Windows 10 is the minimum workstation baseline for Node. Windows 11 is
recommended for the full Desktop experience. Server deployments require
Windows Server 2019 or newer. Windows 7/8/8.1 and older Server releases are
unsupported. The installer checks the OS version before checking WebView2.

The next release ships the complete Windows package for **x64 only**. The x86
package is suspended: the locked browser dependency graph cannot provide a
consistent 32-bit runtime. Historical x86 releases are retained, and update
checks keep platform selection exact rather than sending an x64 installer to
an x86 client. Go 386 cross-compilation can remain as a development check; it
does not constitute a supported complete x86 product.

The installer copies `config.example.yaml` to `config.yaml` only when no config
exists. This Node bootstrap file contains only the listener address. Desktop
preferences stay in `%APPDATA%\DAgents\desktop\settings.json`; LLM and Manage
credentials stay in Node's runtime settings store. Start Menu and optional
Desktop shortcuts launch `bin\dagents-shell.exe` directly with the install root
and bootstrap config pinned to that installation. Login startup is an explicit
unchecked installation task and keeps its previous choice during upgrades.
The installer also seeds policy files, creates the browser runtime directory,
and adds the install paths to the user's `PATH`.

New installs default to the current user's Programs directory because Node's
bootstrap config and `.runtime` data are writable under the install directory.
The installer runs in user mode and rejects Program Files paths or any selected
directory the current user cannot write. It keeps the recorded install path
during upgrades and calls lifecycle commands only when the installed Shell has
the protocol marker shipped by this version. Older unmarked installs are refused
before any command is sent to their Shell; older machine-wide installs under
Program Files are blocked with a migration message. The installer does not move
or delete existing Node data. Uninstall preserves `config.yaml`, the entire
`.runtime` tree (including edits to installer-seeded policy and skills), and the
per-user Desktop settings at `%APPDATA%\DAgents\desktop\settings.json`.

DAgents Desktop requires the Microsoft Edge WebView2 Evergreen Runtime. The
installer checks its documented Edge Update registration and stops with the
Microsoft download page when it is missing. The runtime is not bundled, so a
fully offline first install must provision WebView2 separately beforehand.

## Build

```bash
PLATFORM=windows-amd64 VERSION=0.x.x scripts/ci/assemble_local_assistant_bundle.sh
PLATFORM=windows-amd64 VERSION=0.x.x scripts/ci/build_windows_installer.sh
```

The artifact is named `dagents-local-assistant-windows-amd64-installer-*.exe`
and includes matching x64 Node, Client, Browser, and Desktop binaries.

Install [Inno Setup 6](https://jrsoftware.org/isinfo.php) and set `ISCC` to the
path of `ISCC.exe`.

The package build also requires the matching architecture binaries and staged
bundle from `assemble_local_assistant_bundle.sh`. CI installs Inno Setup before
compiling; the installer's WebView2 prerequisite is checked at runtime and does
not require network access when Evergreen Runtime is already present.

## Installer artwork

The Workbench light theme follows the UI palette and uses the generated assets
in `assets/`. `scripts/generate-wizard-assets.py` rebuilds the wizard bitmaps;
`scripts/generate-tray-icons.py` rebuilds the shared app icons from
`shared/branding/brand-icon.png`.
