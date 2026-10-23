# 2026-10-19 refreshed Windows x64 candidate

This development candidate includes the closed-output-pipe startup fix from [PR #350](https://github.com/DGS-ai-team/DAgents/pull/350), merged at `6a0107c2694da7cbe2571813f7da628f04ba431c`. Its [integration CI](https://github.com/DGS-ai-team/DAgents/actions/runs/39275310959) and [CodeQL](https://github.com/DGS-ai-team/DAgents/actions/runs/39275320492) passed. `VERSION` remains `0.11.0`; no release tag, production installation, or public release was made.

This supersedes the [95e3f98c candidate](release-candidate.md) as the current local acceptance input. Historical results and hashes remain in that record. The [startup investigation](desktop-startup-panic.md) retains the exact per-build reproduction and regression scope.

## Source and package identity

Ignored build area: `.runtime-test-release-candidate-6a0107c2-20261019/`.

The full source archive is 27,306,364 bytes, SHA-256 `09a4194e0418c5a0d6e10b73bb28c5a012626b2487a9884efce8851d7ded3739`. Root compared source trees: Node, Client, Browser, shared code, and runtime seeds are unchanged from `95e3f98c`; Desktop and its shared frontend are unchanged from the final fix `09c6fb1c`. The candidate therefore reuses those verified binaries rather than claiming they were all recompiled from the merge commit. Installer scripts come from the new frozen source.

| Payload | Bytes | SHA-256 |
| --- | ---: | --- |
| Node | 19,192,320 | `735ccd4766a3dd0976819716991343c25db30dbcdb5b1c61a8e20da9cd887b0c` |
| Client | 7,009,280 | `85fd9a7f75f17e3e1a7933ee1c116d310771955106bec65e8decd73f5ac883fb` |
| Desktop | 4,766,720 | `d1bd926a0dcdc12c2b34e690585f1aa483621569399a86468e68094e46d0d7db` |
| Browser | 381,288,752 | `42d02bb5371bc296746f64b4e30822ede4a68d86f0105c8ab76fa3d53e94f0ef` |

Go payloads use Go 1.26.9, Windows amd64, CGO disabled, with version `0.11.0` injected. Desktop is the production-mode x64 MSVC release with Tauri `custom-protocol`, not a development-server or diagnostic-hook build. Browser uses Python 3.13.14 x64, PyInstaller 6.22.3, Playwright 1.63.0, and the frozen Chromium `153.0.8010.12` payload.

The bundle contains 51 files totaling 412,343,879 bytes. Root independently verified every path, size, and checksum, the four executable inputs, and all 62 `SHA256SUMS` entries. Only Desktop differs from the previous bundle; the other 50 files are identical. There are no development databases, actual `config.yaml`, secret-key file, or locks in the bundle. Bundle manifest SHA-256: `a6b00f04c701ca04c2e098a8f266eafca6d485d3148bd49bdd58cce8f8741fec`; candidate manifest SHA-256: `a08235803e5de51e80c9b9c8a99c1f88697a08bb2990c22eb1e62a57a781d446`.

Inno Setup 6.7.3 compiled these installers. Compiler SHA-256: `0a8757031b33777e4c9cbffee40f11a5062b36d25cbe144c1db73b6102b80ad7`.

| Installer | Bytes | SHA-256 |
| --- | ---: | --- |
| `dagents-local-assistant-windows-amd64-installer-0.11.0-qa.exe` | 390,816,653 | `ea918b02b5aaf1f94cabe43cf552e0ff03db26cbd948271dfc7b37c2a57261d3` |
| `dagents-local-assistant-windows-amd64-installer-0.11.0.1-qa.exe` | 390,816,672 | `c7614950fed28fecc40d7d37818b901a2d5cf6016c77ef3cefd9edde39f7a385` |
| `dagents-local-assistant-windows-amd64-installer-0.11.0.exe` | 390,818,226 | `b5ad730c72ec3a4c7fcd09a14e5b6a7a29dbc79333c09279d227a5970c36843a` |

The production installer was compiled only. The QA upgrade package has `packageVersion=0.11.0.1`, `bundleVersion=0.11.0`, and `packageVersionKind=synthetic-qa-sequence`: it exercises installer lifecycle with the same product payload, not migration from an older product.

## Installed native acceptance

The user authorized this administrator development machine. Environment remains Windows 11 Pro build 26200 with WebView2 `155.0.4283.62`. New fixture: `.runtime-test-release-install-6a0107c2-20261019/DAgents QA 安装 Fixture`; endpoint `127.0.0.1:18972`; QA AppId `{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}`. Earlier fixtures were not reused.

| Stage | Observed result |
| --- | --- |
| Install and seed | Both stages passed `packaging/windows/test-isolated-installer.ps1`. Installer sidecar, installed executable hashes, and QA registry identity matched. Listen-only config, sample policy, sentinel, and fixture Desktop settings were seeded. |
| Owned startup | Installed Shell started the fixture-owned Node. Health reported version `0.11.0`; the final fixed Desktop opened its embedded native UI. No development frontend server was used. |
| Setup and use | Root completed the two-page native onboarding, then selected the fixture's existing keyless Mock profile through the settings API. Root created private ordinary Agent `agt-45b8ecedb924e0bc`, named `候选升级验收`, through the native wizard. Sending `QA-CANDIDATE-6A0107C2：这条对话需要在升级后保留。` produced both user and Mock assistant messages and returned to idle. No external Provider was called. |
| Running upgrade | With Node and Desktop running, Stage Upgrade passed. Installer hooks quit Desktop and stopped its owned Node. Payload, sample files, Node settings logical digest, and production-side-effect guards passed. |
| Restart and recovery | The manifest-pinned helper started the upgraded installation. Node identity, Agent identity, both complete transcript entries, active Mock profile, and completed onboarding were preserved. Root inspected the native window and confirmed the selected Agent and both messages recovered without another onboarding flow. |
| Running uninstall and verify | With the recovered Node and Desktop running, Stage Uninstall and Stage Verify passed. Product binaries, launcher, uninstaller, and QA registration were removed; sample config/policy/Desktop settings and runtime data remained. No QA endpoint remained listening. |

The independent read-only snapshot covers all nine fixture SQLite databases, hashing schema and ordered user-table rows, plus the two history-scope files. Snapshots before upgrade, after cold upgrade, before uninstall, and after uninstall are byte-identical: SHA-256 `60b4fcd121985805cbc4eff20d944369a0aeb4f243fc9c4e9df8e3b1561bcae6`. Full transcript equality was checked separately after restart. Snapshot files contain hashes, not row or history contents. No LLM key was configured or secret-key file read; populated credential recovery is not established by this run.

Production autostart, user PATH, shortcut, Desktop preferences, and uninstall-registration hash guards passed. The unrelated development Node on `18977` retained identity `82cc53be-011b-4638-bbcc-374564fddf20`. No second unrelated Desktop was open during this run, so concurrent second-Desktop preservation is not claimed. QA user data remains in the fixture after uninstall.

The local `open-qa.ps1` helper (SHA-256 `f7ba68ee7dd6ed65fc1fab5d2d99a157ff880dc316d4593fe2f02baf9a4d2228`) passed real Windows PowerShell 5.1 execution. It pins fixture, manifests, payloads, registry, listener, owned-process status, and isolated WebView2/profile paths. During preparation, helper-only fixes addressed UTF-8 Chinese paths, canonical `\\?\` paths, and CLI capture pipes inherited by a child Node. An early helper waited indefinitely after successfully starting Node; root stopped only that verified helper process, then resumed with the corrected helper. This was not a product crash or a failed installer stage. No automatic approval rejection occurred in this refreshed acceptance run.

Detailed fixture state, Inno logs, logical snapshots, and pre/post-upgrade hydration evidence remain in the ignored fixture directory. Raw runtime data and installers are not committed.

## Packaged Browser smoke

Root ran the actual frozen Browser executable above through `.runtime-test-browser-package-20261019/run-browser-smoke.ps1` (SHA-256 `273a0365d46370e1a1fb2fa45733e1b00d89316ca02503bc409c2e6df342802d`). It resides in the prior candidate directory but is byte-identical to this candidate's Browser. Run ID: `92d1e531f5404970a291b160f0504fcd`.

The service ran in a fresh working directory using its default `.runtime`, isolated user/temp directories, and no `PLAYWRIGHT_BROWSERS_PATH` override or reused browser profile/cache. Tests used Browser port `18974` and a local static page on `18975`; no Node or Desktop was started by this runner.

- Health and protocol-v2 ping passed with driver `playwright-v2`.
- Starting the packaged headless Chromium, navigating to the local page, checking its title, and evaluating its DOM marker passed.
- Screenshot view produced a valid 1280×720 PNG; root visually confirmed `packaged-browser-ok`. PNG SHA-256: `a5498dbbba7646c1fb65b8f858ef11bc4a05300e60dc1fc523396f333ec4bdbc`.
- Session stop succeeded; a subsequent view returned the expected `browser_not_started`, while service health remained good.
- Cleanup terminated only the two owned test processes. No cleanup errors or listeners on `18974`/`18975` remained.

The saved `result.json` reports `passed`. This establishes standalone packaged service/embedded Chromium operation. It does not establish Node browser-tool integration, Desktop browser-resource viewing, headed operation, external website compatibility, or a Linux package.

## Release decision

The refreshed final payload has now passed local administrator QA installation, native Mock use, running synthetic upgrade, recovery, running uninstall, and runtime preservation. The known output-pipe crash fix is included and no new product defect was found in these paths.

Stable-release approval remains open in [release readiness](../../../../release-readiness.md): standard-user and production shortcut/autostart/PATH branches, actual old-release migration and failure recovery, populated credentials/real Provider/MCP, SSH/Linux integration, at least 24-hour Auto durability, and formal version/release-asset validation. This record closes the listed local checks, not those broader gates.
