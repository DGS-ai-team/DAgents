# 2026-10-19 Windows x64 candidate

This is a development acceptance build, not a published release. Product source is frozen at merged PR #348, `95e3f98cbce0a210e41a15590e45d42e353c119a`. Its [integration CI](https://github.com/DGS-ai-team/DAgents/actions/runs/39202528888) and [CodeQL](https://github.com/DGS-ai-team/DAgents/actions/runs/39202528519) passed. `VERSION` remains `0.11.0`; no version bump, release tag, or release was created.

The ignored build area is `.runtime-test-release-candidate-20261019/`. Installation QA tooling changes made after the product freeze are separate from the product payload. The build inputs and installer checksums below identify the current candidate; older October 14/17 results do not establish this candidate's installation behavior.

## Product inputs

| Input | Build | Bytes | SHA-256 |
| --- | --- | ---: | --- |
| Node | Go 1.26.9, Windows amd64, CGO disabled | 19,192,320 | `735ccd4766a3dd0976819716991343c25db30dbcdb5b1c61a8e20da9cd887b0c` |
| Client | Go 1.26.9, Windows amd64, CGO disabled | 7,009,280 | `85fd9a7f75f17e3e1a7933ee1c116d310771955106bec65e8decd73f5ac883fb` |
| Desktop | Tauri x64 MSVC release, Rust/Cargo 1.97.1 | 4,798,976 | `7c05f8e37e9683514e25d545756e6543e13550352a1a2758bdb627a5201f7493` |
| Browser | Python 3.13.14 x64, PyInstaller 6.22.3, Playwright 1.63.0 | 381,288,752 | `42d02bb5371bc296746f64b4e30822ede4a68d86f0105c8ab76fa3d53e94f0ef` |

The initial Go build omitted the release version injection and was rejected during root review. Both Go binaries were rebuilt using the frozen source and the official static build script. Final `go version -m` records contain `-X github.com/DGS-ai-team/DAgents/node/internal/version.Version=0.11.0`, `GOOS=windows`, `GOARCH=amd64`, and `CGO_ENABLED=0`. These binaries do not embed a VCS revision; source provenance is recorded externally rather than claimed as embedded metadata. Earlier failed build attempts and the interrupted installer compile are not accepted outputs.

Desktop was rebuilt with its frontend; its FileVersion and ProductVersion are both `0.11.0`. Browser was repackaged from the frozen source and lockfile. It reuses the previously prepared Chromium `153.0.8010.12` dependency payload, including headless shell; the Browser executable itself is newly built. Neither Browser nor Chromium was launched during build verification.

Root independently compared the four assembled executables with the input hashes. The bundle contains 51 files totaling 412,376,135 bytes. All 24 files under its `.runtime` tree match the repository's `packaging/runtime` seed files. No development database, actual `config.yaml`, Desktop settings, or lock file was found in the bundle.

Root also verified all 58 artifact records and 61 entries in `SHA256SUMS`, the source archive checksum, all 15 recorded source/tool inputs, and the exact 51-file bundle path set. The baseline manifest SHA-256 is `ba9ff8ee142ab08b8753937a263533517891a9108187f4a3a3a67218143f0a10`; `SHA256SUMS` SHA-256 is `296664bc463b7f6a42b913cb2e2693491fe55d3e43ab0210a0028adfaa6422a1`. Subsequent synthetic QA package inputs are recorded separately rather than altering this baseline.

## Installer compilation

Portable Inno Setup 6.7.3 compiled both branches. Compiler SHA-256: `0a8757031b33777e4c9cbffee40f11a5062b36d25cbe144c1db73b6102b80ad7`.

Both installer wrappers have PE machine `0x014c`; their Inno architecture directives require x64-compatible Windows. The four product executables have PE machine `0x8664`. The wrapper architecture is not an x86 product package.

| Installer | Bytes | SHA-256 |
| --- | ---: | --- |
| `dagents-local-assistant-windows-amd64-installer-0.11.0-qa.exe` | 390,829,090 | `4ee98311843e49783f6c828f825e33a6eabee8831d0b31cdfe91bbfcc631e337` |
| `dagents-local-assistant-windows-amd64-installer-0.11.0.exe` | 390,830,663 | `e63a75befcca96436d29e0f758dc14c6046c88c27bb8fde5b11840f76bc21bc8` |

These are compile results only. A later QA-only package sequence may exercise the upgrade lifecycle with the same immutable product payload; such a sequence is not an old-product migration or a new product version.

## Installation scope and remaining gates

The user authorized using this development machine for installation acceptance. Read-only preflight found Windows 11 Pro build 26200, an administrator account, PowerShell 7.6.5, registered WebView2 `155.0.4283.62`, and unused QA port `18972`. Use a separate QA AppId, installation directory, bootstrap config, and Desktop preferences.

The initial build verification launched none of the product applications or installers. The following local acceptance run subsequently used the same hashed candidate. Fixture retention checks cover sample config/policy/sentinel/Desktop files and the Node settings logical digest; they do not establish preservation of every LLM secret, history, workspace, or uninitialized database.

A local administrator fixture does not establish clean standard-user installation. QA builds omit production shortcuts, autostart, and PATH changes. Those branches, previous-release migration, recovery failures, actual remote services, and 24-hour Auto durability remain separate gates in [release readiness](../../../../release-readiness.md).

## Local installation acceptance

Fixture: `.runtime-test-release-install-20261019/DAgents QA 安装 Fixture`, endpoint `127.0.0.1:18972`, QA AppId `{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}`. Each stage uses the corrected `packaging/windows/test-isolated-installer.ps1`; evidence and Inno logs remain in the ignored fixture directory.

| Stage | Result and boundary |
| --- | --- |
| Initial installation | Passed. Inno exited successfully, canonical installed hashes matched the selected manifest, QA registry identity matched the fixture, and production Run/PATH/shortcut/preferences/uninstall snapshots stayed unchanged. |
| Seed | Passed. Fixture-only sentinel, modified policy, listen-only `18972` config, and isolated Desktop preferences were created without production credentials. |
| Owned Node start | Passed. The installed Shell started its owned Node; `/health` reported `0.11.0`. |
| Native setup and use | Passed with the later `Start-Process` instance. Root completed both setup pages, created a private ordinary Agent through the native wizard, selected the default Mock profile, and sent `QA-INSTALL-20261019`. Both user and Mock assistant messages appeared; the Agent returned to completed/idle. This is not a real Provider test. |
| Running upgrade | Passed. A separate synthetic QA package `0.11.0.1` used unchanged product bundle `0.11.0`. Installer hooks quit the fixture Desktop and stopped its owned Node. Installed payload hashes, sample persistent files, logical Node settings digest, and production snapshots passed. The other development Node on `18977` retained its identity and its existing Desktop remained running. |
| Restart and history recovery | Pending. Automatic approval rejected the restart command with `blocked by policy`, without a rule explanation. A saved pre-upgrade transcript and Node identity are available for comparison after manual restart. |
| Uninstall and final persistence | Not executed. Requires the restarted fixture instances and remaining checks. |

The synthetic package is 390,829,109 bytes, SHA-256 `5e0934227a3549576cce1eebf1d87318ce7bd52ef2b56c815e308b16c36744ee`. Its sidecar records `packageVersion=0.11.0.1`, `bundleVersion=0.11.0`, and `packageVersionKind=synthetic-qa-sequence`. Root verified all seven entries in `upgrade-sequence/SHA256SUMS`. The updated compiler script's SHA-256 is `5c2403d115bf4bdff06464c1fd4ec544ff5dfcbf6d3d129be899e5460560417b`; the Inno source hash remains the frozen source's `b97784d3a0dfc3287b01173a2425c9fe02bd9b0e229e67812616d59c1defebe1`.

The manual restart helper is `.runtime-test-release-install-20261019/open-upgraded-qa.ps1`, SHA-256 `209343b419a98c60082f18d135740ad8bb9a97dd99f0b56742766ffb1dc5853b`. Windows PowerShell 5.1 read-only `-PreflightOnly` passed against the real upgraded fixture and confirmed no matching processes were running; it launched none. The helper pins fixture paths, versions, payload hashes, and endpoint before starting anything. The tracked acceptance contracts passed 30 PowerShell assertions and the shell compiler-stub checks; CI now runs these Windows harness checks separately from product tests.

## Unresolved startup crash

The first direct PowerShell `&` GUI launch faulted shortly after creating a window. Windows Application Error event 1000 recorded exception `0xc0000409`, offset `0x262135`, PID 31892 at 13:29:49. A later independent `Start-Process` launch, PID 31868, stayed running and completed the native setup/use checks before the installer quit it normally during upgrade.

The faulting executable matches the Desktop hash above. Its CodeView PDB GUID is `{F6E12B19-3759-4288-83DC-97B4A43C4484}`, age 1; no matching PDB is available locally. The existing older PDB does not match. Available symbol tooling could not map the offset. The local WER dump was left in place and not transmitted. Logs only establish that startup had begun; neither PowerShell Job cleanup nor a particular product initialization step is a proven cause.

Keep this as an open release risk. Successful setup and upgrade do not close the crash investigation or justify stable-release approval.
