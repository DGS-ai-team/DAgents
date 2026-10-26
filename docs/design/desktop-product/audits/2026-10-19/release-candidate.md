# 2026-10-19 Windows x64 candidate

Historical baseline: the current local candidate and installation results are recorded in [the refreshed 6a0107c2 audit](release-candidate-6a0107c2.md). The original hashes and observations below are retained unchanged.

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
| Restart and history recovery | Passed after the user manually ran the reviewed restart helper. Node identity and both transcript messages exactly matched the saved pre-upgrade values. Root inspected the native window and confirmed the selected Agent, history, and Mock model recovered; onboarding remained complete. |
| Uninstall and final persistence | Passed with Node and Desktop running. Inno removed the four application binaries, launcher, uninstaller, and QA registry entry, while sample config/policy/Desktop preferences and the logical Node settings digest remained equal. Stage Verify passed. QA processes and endpoint were gone; the other Node on `18977` retained its identity. |

The synthetic package is 390,829,109 bytes, SHA-256 `5e0934227a3549576cce1eebf1d87318ce7bd52ef2b56c815e308b16c36744ee`. Its sidecar records `packageVersion=0.11.0.1`, `bundleVersion=0.11.0`, and `packageVersionKind=synthetic-qa-sequence`. Root verified all seven entries in `upgrade-sequence/SHA256SUMS`. The updated compiler script's SHA-256 is `5c2403d115bf4bdff06464c1fd4ec544ff5dfcbf6d3d129be899e5460560417b`; the Inno source hash remains the frozen source's `b97784d3a0dfc3287b01173a2425c9fe02bd9b0e229e67812616d59c1defebe1`.

The manual restart helper is `.runtime-test-release-install-20261019/open-upgraded-qa.ps1`, SHA-256 `209343b419a98c60082f18d135740ad8bb9a97dd99f0b56742766ffb1dc5853b`. Windows PowerShell 5.1 read-only `-PreflightOnly` passed against the real upgraded fixture and confirmed no matching processes were running; it launched none. The helper pins fixture paths, versions, payload hashes, and endpoint before starting anything. The tracked acceptance contracts passed 30 PowerShell assertions and the shell compiler-stub checks; CI now runs these Windows harness checks separately from product tests.

Automatic approval had rejected the restart command with `blocked by policy`, without explaining a rule. The user then ran the helper; this manual step is part of the evidence, not an automated-start pass.

An additional read-only SQLite transaction snapshot covered all nine fixture databases. It hashes schema and ordered user-table rows, including BLOB values, without outputting contents. Before and after uninstall snapshots were identical: SHA-256 `0dc4a73725da916c49d44d842af32b4e02b1252e09b8ec92e368965690f5f769`. The two history-scope files (the Agent's message JSONL and a seed placeholder) also retained their hashes. No API keys were configured in these profiles, and the secret-key file was not read; this does not establish credential recovery or preservation of a populated production workspace. Logs, process metadata, and lock files are outside this comparison.

The unrelated Desktop was absent from the observed window inventory before uninstall, so this uninstall run makes no claim about preserving a concurrently open second Desktop. The earlier upgrade run did verify that separate Desktop remained running. Production-setting hash guards passed throughout both stages. Detailed JSON evidence is under the fixture's `evidence/`, and all QA user data was retained after uninstall.

## Startup crash and follow-up

The first direct PowerShell `&` GUI launch faulted shortly after creating a window. Windows Application Error event 1000 recorded exception `0xc0000409`, offset `0x262135`, PID 31892 at 13:29:49. A later independent `Start-Process` launch, PID 31868, stayed running and completed the native setup/use checks before the installer quit it normally during upgrade.

The faulting executable matches the Desktop hash above. Its CodeView PDB GUID is `{F6E12B19-3759-4288-83DC-97B4A43C4484}`, age 1. The first investigation checked the older `target/release` PDB and could not map the offset. Subsequent review found the matching PDB under the actual `target/x86_64-pc-windows-msvc/release` directory: 2,953,216 bytes, SHA-256 `06484d3275c82c8cf23b277a3a09a5ac27a2cf53754bcc2a0e1598ea050c1de8`. Its GUID and age exactly match the dump's CodeView record.

LLVM symbolization maps the fault RVA to Rust `abort_internal`, with inline locations through `process::abort`, `__rust_abort`, `panic_abort::__rust_start_panic`, and `std::panicking::rust_panic`. Exception subcode 7 is the Windows SDK's `FAST_FAIL_FATAL_APP_EXIT`. This confirms a panic-abort exit, rather than identifying the initiating operation.

Several raw stack words match the encoding of an OS error 232 under the [matching Rust source's bit-packed I/O error representation](https://github.com/rust-lang/rust/blob/8bab26f4f68e0e26f0bb7960be334d5b520ea452/library/std/src/io/error/repr_bitpacked.rs). Windows defines 232 as [ERROR_NO_DATA, a closing pipe](https://learn.microsoft.com/en-us/windows/win32/debug/system-error-codes--0-499-). Stack words also symbolize to `print_to` / `_eprint`, but these are address candidates, not verified caller frames. The attempted offline walk did not produce a trustworthy full stack; a stderr write failure remains a hypothesis. No verified business call site or panic message was recovered, and no product source patch was made on that basis. The local WER dump was not transmitted.

At this baseline investigation stage, the crash remained open; successful setup and upgrade alone did not close it. A subsequent [controlled reproduction and fix](desktop-startup-panic.md) captured the panic message and reliable `events::sync_agents` call stack. Closing the stderr reader reproduces the abort in this exact release candidate; the fix tolerates failed output writes. A follow-up covers the same stdout failure in CLI status commands.

The fix source is `09c6fb1c`. Its production-mode x64 Desktop (SHA-256 `d1bd926a0dcdc12c2b34e690585f1aa483621569399a86468e68094e46d0d7db`) passed the closed-stderr process regression and stdout CLI contract/closed-pipe checks. The preceding stderr-only fix also passed a 120-second closed-pipe run with native embedded-UI interaction. See the linked audit for exact per-build scope. At that stage the fixed executable had not yet been assembled into a new candidate. The subsequent [6a0107c2 candidate](release-candidate-6a0107c2.md) includes this exact executable and has completed local QA installation, native use, upgrade, recovery, and uninstall. Broader release gates remain open. The original candidate hashes and installation evidence above remain unchanged.
