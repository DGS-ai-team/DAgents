# Windows install and upgrade acceptance

Updated: 2026-10-14

This is the Windows acceptance checklist for the Node API plus Tauri Desktop
package. It is intended for a disposable Windows profile or VM. Do not run the
upgrade/uninstall cases against a developer profile with real Node data.

## Package and prerequisite checks

- Build both `windows-amd64` and `windows-386` staging bundles, then compile
  each with Inno Setup 6 (`ISCC.exe`). Confirm the installer includes
  `dagents-node.exe`, `dagents-client.exe`, `dagents-browser.exe`,
  `dagents-shell.exe`, `dagents.cmd`, bootstrap config, and seed files.
- With WebView2 Evergreen Runtime registered, the installer opens normally.
  With both machine and current-user registrations absent, setup stops before
  installation and shows Microsoft's WebView2 download page.
- In an offline environment, setup works when WebView2 was provisioned first.
  The DAgents package does not contain the Evergreen Runtime installer.

## Isolated command-wrapper fixture (passed 2026-10-14)

Root ran the final bundled shell and Node binaries against isolated fixture
paths containing spaces and Chinese characters. The fixture used a separate
Desktop JSON settings file and a listen-only Node config. Results:

- `unknown` returned exit code 2; Node online status returned 0 and offline
  status returned 1; Shell offline status returned 1.
- Owned background Node start returned 0 and preserved the same Node ID across
  status checks; owned stop returned 0.
- A repeated Desktop launch with case and config-path variations kept one GUI
  and one Node. Desktop Quit left Node running. CLI quit affected only that
  install's Desktop; a second install's Desktop and Node stayed running.
- The read-only ownership check did not claim an external Node. The attempted
  external stop was refused and is recorded as a refusal, not a pass.
- The fixture services were stopped and cleaned up by root after the checks.

These results cover command dispatch and lifecycle behavior only. They do not
validate an installer build, installation, upgrade, or uninstall.

## Fresh install

- Install as a standard user. The default directory is under that user's
  Programs directory. No elevation prompt appears.
- Enter an install directory containing spaces and Chinese characters. Confirm
  `dagents.cmd`, Node, and Desktop Shell launch with the correct working
  directory and config file.
- Select Program Files or a non-writable custom directory. Setup must stop
  before replacing files and explain that the install needs a user-writable
  path. This also covers old machine-wide installs; setup must not silently
  move their config or `.runtime` data.
- Verify first install creates `config.yaml` only from the listen-only example.
  It must not overwrite an existing Node config, `.runtime` database, policy,
  or `%APPDATA%\DAgents\desktop\settings.json`.
- Confirm the installer and Desktop agree on login autostart: one Shell starts
  at login, while explicit Shell Quit leaves a managed Node running and close
  hides Desktop to the tray.

## Upgrade and removal

- Seed the disposable profile with a non-default `config.yaml`, `.runtime`
  database and policy, plus Desktop `settings.json`. Upgrade in place and
  confirm every file remains unchanged.
- Upgrade while Node and Desktop are running. Only a Node owned by this install
  may be stopped; an external Node and another Desktop install must remain
  running. This install's Desktop must quit through its shell command before
  the owned Node stop. The upgrade must not rely on an unverified PID or
  process name.
- Use `dagents.cmd node` to start the owned Node and confirm it creates the lifecycle owner record. `dagents.cmd node --foreground` is external and must be stopped with Ctrl+C in its own terminal.
- Attempt an in-place upgrade from an unmarked older installation. Setup must refuse before invoking the old Shell CLI and explain the backup/uninstall path.
- Start an unowned Node from this install's `dagents-node.exe` and attempt an upgrade. Ownership stop must be a no-op; if Windows still holds the binary,
  setup must fail with the normal file-in-use error rather than closing it.
- Uninstall with an owned Node, external Node, and another Desktop instance
  present in separate runs. Only the owned Node and this install's Desktop may
  stop. External Node data and another install's autostart entry must remain.
- Confirm uninstall removes this install's autostart and shortcuts, while user
  `config.yaml`, the whole `.runtime` tree (including edits to seeded policy,
  skills, and documentation), and Desktop `settings.json` remain. Installer
  payload files outside `.runtime` can be removed with the application.

## Isolated QA installer

Compile the isolated candidate only with the explicit QA switch. Keep the
fixture directory under this checkout and pass it with `/DIR`; the package
uses a separate AppId (`{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}`) and stores its
Desktop settings at `<installRoot>\qa-desktop-settings.json`. The QA build
does not create shortcuts, run the app after setup, change login autostart, or
edit the user PATH. WebView2 and Windows version checks remain enabled.

```bash
export ISCC="$PWD/.runtime-test-install-delivery/tools/inno-6.7.3/ISCC.exe"
export INSTALLER_QA=1 PLATFORM=windows-amd64 VERSION=0.11.0.1
bash scripts/ci/build_windows_installer.sh
```

```powershell
$fixture = Join-Path (Resolve-Path .).Path '.runtime-test-install-delivery\qa-install'
$installer = Resolve-Path 'dist-installer\dagents-local-assistant-windows-amd64-installer-0.11.0.1-qa.exe'
$dirArg = '/DIR="' + $fixture + '"'
& $installer /VERYSILENT /SUPPRESSMSGBOXES /NORESTART $dirArg
```

This is a disposable QA installer for exercising shared installer/lifecycle
paths. A successful compile or fixture run does not establish full production
upgrade, uninstall, shortcut, autostart, or release-package acceptance.
The build script writes a .manifest.json sidecar with the QA macro marker,
QA AppId, version, installer filename and SHA-256, plus names and SHA-256
hashes for every executable in the staged `bin` directory. The fixture runner
requires the sidecar and verifies the QA identity and installer hash.

## Build results for this checkout

On 2026-10-14, the complete `windows-amd64` staging bundle was assembled with
Node, Client, Desktop Shell, and Browser executables. Inno Setup 6.7.3 compiled
isolated QA packages `0.11.0.1` and `0.11.0.2`, and the normal package
`0.11.0.2`. Full compiler logs are retained beside the packages in
`dist-installer`; each QA package has a manifest listing the staged executable
hashes. Root then completed an isolated first install, a package-metadata upgrade,
native Desktop restart, uninstall and preserved-data verification. The first
upgrade candidate's bundle VERSION mismatch was subsequently fixed and both
final 0.11.0.2 branches were rebuilt; the final candidate's complete rerun was
blocked by automatic approval review. These results must not be conflated.
Evidence and remaining gaps are recorded in
[the Oct 14 acceptance report](../../docs/design/desktop-product/audits/2026-10-14/install-upgrade/README.md).
Production shortcuts/autostart/PATH, ordinary-user VM coverage and fault rollback
remain untested. The Browser executable's `--help` check passed, but its service
startup test was blocked and not executed. Runtime WebView2 remains an external
prerequisite and is deliberately not downloaded during DAgents setup.
