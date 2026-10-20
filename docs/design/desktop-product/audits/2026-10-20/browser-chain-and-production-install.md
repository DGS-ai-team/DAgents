# 2026-10-20 packaged Browser chain and production installation

These checks extend the [6a0107c2 candidate audit](https://github.com/DGS-ai-team/DAgents/blob/ee5d4a20a101d5d48eb5037b56f2ba59aa613ae4/docs/design/desktop-product/audits/2026-10-19/release-candidate-6a0107c2.md), submitted separately in PR #351. They use its verified x64 payloads and production installer, still carrying development version `0.11.0`. They do not publish a release or establish standard-user acceptance. The test account is an elevated administrator on the previously authorized Windows 11 development machine.

## Node → Browser → native Desktop

The successful ignored run is `.runtime-test-node-browser-desktop-20261020/runs/run-5dc9828a453d424ab2cb03b1b9f37426/`. Inputs are the candidate's actual Node (`735ccd47…887b0c`), Browser (`42d02bb5…4f0ef`), and Desktop (`d1bd926a…d7db`) binaries; full hashes are in the linked candidate record. No product source changed for this test.

The runner starts a local deterministic OpenAI-compatible SSE server and static page, then the packaged Browser sidecar and Node. It uses fresh runtime/profile directories and machine-only child PATH, without existing Browser profiles, provider credentials, or a Playwright browser-path override. Ports are Node `18968`, synthetic LLM `18969`, static page `18970`, and Browser `18967`.

The real Node settings API configured the local synthetic profile and Browser service. A private ordinary Agent `agt-3f4b21e1930b9060` received a normal message through `/v1/messages`. Its actual tool registry executed this sequence:

1. `browser_call` started its Agent-owned browser session.
2. `browser_call` navigated to the local static page.
3. `browser_evaluate` returned the page's `node-browser-package-ok` marker.
4. The synthetic provider supplied the final assistant response.

All three hydrated tool results were JSON with `ok=true`, in the expected order. The evaluate result and final assistant text matched the marker. The synthetic server received exactly four streaming completion requests with the expected model, synthetic authorization, and both Browser tools advertised. It does not retain request bodies or credentials.

The Node read-only `/v1/agents/{agent_id}/browser/view` API returned `image/png`, `page_1`, the expected local URL/title, and a valid screenshot. PNG: 5,750 bytes, SHA-256 `4a6417f89ce74cfce88cf942e67bc14d2005dbba12e5e59d33335fbd2e2eea5c`.

Root then used Computer Use on the real Tauri window. It displayed three completed tools and the final response. Opening **资源 → 查看浏览器** showed the correct title, URL, and marker in the read-only screenshot. Manual refresh worked; the resource message dock expanded upward to show the final message and latest tool, leaving the input below. The window remained functional after uninstalling the separate production test installation.

Desktop used `connection_mode=tunnel` with this local endpoint to avoid managing the externally started test Node. This is not an actual SSH connection or Linux test. The sidecar was started by the runner, not automatically managed by Node or Desktop. The provider was scripted, so this tests real protocol/tool/resource integration rather than a model's planning ability or an external website.

Root requested this exact Desktop instance to quit, observed its process exit, and signaled the run marker. The runner then terminated only its three owned process trees. Saved status is `desktop-reviewed-cleanup`, with no error and all children exited; their forced cleanup exits are `-1`, not claimed as graceful service shutdown. The separate October 19 sidecar smoke covers normal browser-session stop.

Runner SHA-256: `b3d6da050007dca80aef045be4181aa374009e0d0bb19eff0fb0fb478d2dd5e2`; Desktop helper: `80ca2eceead0dbdd5cd2cba238702756bc3597ab5dcc7f545de507be1265e25d`; synthetic server: `95b09345cd1d0bde81acefc0c7c83f69db64c5d65a3c150ca0697527d2ea52e6`. The initial failed run `552e91ab4f644db3990a47716be7d42a` is retained: the helper's `$Args` parameter collided with PowerShell's automatic variable and Python entered its REPL. Renaming that helper parameter fixed startup; no Node or Browser product process had started in that failed attempt.

## Actual production installer branches

Ignored fixture: `.runtime-test-production-install-20261020/DAgents 正式安装验收 Fixture`; isolated Node endpoint `127.0.0.1:18971`. The actual production installer SHA-256 is `b5ad730c72ec3a4c7fcd09a14e5b6a7a29dbc79333c09279d227a5970c36843a`, not the QA macro build. Its production AppId was absent in HKCU/HKLM, both registry views, before installation.

Before launch, the helper recorded original Run and user PATH values/types and hashes of all affected shortcuts. The real Desktop preferences were hashed only. Installer descendants received separate APPDATA, LOCALAPPDATA, WebView2 data, and test config paths. The original Run and relevant shortcuts were absent; original user PATH had 17 entries and registry kind `String`.

| Check | Actual result |
| --- | --- |
| Program Files refusal | Installer returned `7` for a unique nonexistent Program Files target; no target directory or tracked global state changed. |
| Ordinary file as directory | Installer returned `7`; the file hash and tracked global state were unchanged. |
| Initial production installation | Returned `0`; all four installed executable hashes matched the candidate. Postinstall started this fixture's Desktop and owned Node. |
| Default tasks | Start Menu Desktop link target, complete arguments, working directory, and icon were independently checked. Desktop icon and Run autostart were absent. User PATH equaled its original value plus exactly the two fixture paths; registry kind became `ExpandString`. |
| Explicit enable | Enabling desktop icon and autostart created the expected shortcut and exact fixture-owned Run command. |
| Preserve choices | Reinstalling the same package without `/TASKS` preserved both enabled choices. This is maintenance with the same payload/version, not old-release migration. |
| Explicit disable | Disabling autostart removed the owned Run entry and retained the selected Desktop shortcut. |
| Re-enable and running uninstall | Autostart was re-enabled and verified before uninstall. The real uninstaller returned `0`, removed this installation's shortcuts, registration and app binaries, and stopped its owned idle Node/Desktop. The unrelated Browser QA Desktop/Node and the existing development Node were preserved. PATH cleanup failed the independent postcondition described below. |

The requested `/GROUP` value did not become the actual Start Menu group: the installation log and registry both identify `DAgents`. The snapshot already covered this default group as well as the requested group. Verification was corrected to the observed group; this record does not infer an undocumented cause or claim custom-group selection worked.

The seeded Node config was normalized on first startup by `BootstrapNodeSettings` → `SaveBootstrapFile`. Its listener stayed `127.0.0.1:18971`; the byte change was the known canonical header/serialization. A one-shot acceptance step checked the complete canonical bytes, Node identity, ownership record and running executable. Original seed hash `376c859e4f6629d08e4a0e51c83f1d3362ec63358e23f1574c2582b2ec7924d5` remains in the snapshot; canonical hash `6a7922ce77080c19d71194de9e88c9be9988288a959b466a46cecec38a94e806` is recorded separately. No product patch was needed for this helper expectation.

Root completed native onboarding without a provider request, then used the fixture settings API to select its keyless default Mock profile. Agent `agt-66a031cac49ac55a` completed a two-message Mock conversation, and `qa-preserve.txt` was created in its private workspace. After maintenance, full transcript equality passed and the native window recovered that Agent, both messages and the Mock model without onboarding.

All four logical runtime snapshots—before maintenance, after maintenance, before uninstall, after uninstall—are identical: nine SQLite databases and three history/workspace-scope files, SHA-256 `d7972628b51cd0de260ea6b7f01ba32812c20de84cbe1e2e577ab75967e48895`. The snapshots hash schema/rows and file contents without outputting them. This is synthetic data; it does not establish real credential or populated user-workspace recovery.

## PATH failure and host restoration

The production uninstaller's process exit was successful, but its PATH postcondition failed. The original 17 entries remained in order; the installation root was not removed, and the added external-tools path was split into `C:\Us` and `rs\…\externaltools`. This is an actual installer cleanup defect, not a passed uninstall gate or a reason to weaken the helper assertion.

Root restored user PATH only after matching the full current value to the exact original 17-entry prefix plus those three known fixture-derived tokens, with registry kind `ExpandString`. The original complete value and original `String` kind were restored and read back. Run, shortcuts, real Desktop settings hash, and absent production registration were also checked against the original snapshot. The guarded restoration is recorded in `evidence/manual-path-restoration.json`; it does not turn the failed installer cleanup into a pass. The preserved runtime data remains in the fixture.

The failed candidate is retained as evidence. The follow-up below records the fix separately; a successful process exit alone does not satisfy uninstall acceptance. Standard-user installation, fresh-login autostart execution, missing WebView2, real Provider/MCP, SSH/Linux, and 24-hour Auto durability remain separate gates in [release readiness](../../../../release-readiness.md).

## PATH correction and independent regression

The old production function reproduced the Unicode-path corruption in a minimal real Inno uninstaller. It combined `Pos`, `Copy`, and `Delete` to tokenize the PATH. The correction uses typed `StringSplit` / `StringJoin`, filters only matching installation paths, and preserves other tokens verbatim, including empty entries, ordering, and duplicates. Root-path normalization also keeps `C:\` distinct from drive-relative `C:`. The reproduction establishes the broken behavior; the exact internal overload/index conversion in the Inno engine is not asserted as a proven cause.

`packaging/windows/tests/test-path-cleanup-inno.ps1` extracts the actual production function into an inert Inno install/uninstall harness. It checks Unicode/space paths with repeated owned tokens and empty tokens, a 17-token unrelated prefix, and the drive-root distinction. The harness verifies exact returned strings, unchanged raw HKCU PATH hash/type, and removal of its own uninstall registration in both registry views. It does not modify PATH. The same harness against the old production function compiled and ran, then failed behavior assertions (`simple=0`, `long=0`, 20 remaining tokens), providing a negative control.

Root independently reran the final three-case Inno regression successfully. The existing 30 isolated-installer assertions and canonical installer/package-version contract also passed. CI now runs the real Inno regression on Windows before compiling installer variants. These focused checks cover the installer-only change; they do not replace actual production lifecycle or migration checks.

## Existing policy during unattended recovery

The [official-release migration test](v011-production-migration.md) exposed another actual installer defect: restoring an existing `.runtime/policy` before `/VERYSILENT /SUPPRESSMSGBOXES` setup caused the ordinary policy-overwrite `MsgBox` to wait indefinitely. The correction uses `SuppressibleMsgBox` with `IDNO` as its unattended default, preserving existing policy. The interactive Yes/No branch remains in the source; manual selection of both buttons is not claimed as tested here.

The new `test-silent-policy-preservation-inno.ps1` extracts both actual production functions, `PolicyExistsAt` and `NextButtonClick`, into a real Inno fixture. It requires an existing policy, confirms the production decision branch ran, checks that overwrite stayed false and file contents stayed unchanged, then uninstalls and checks its temporary registration is gone. The runner has a bounded process-tree timeout. Root independently reran this check and the final PATH regression successfully. Both are included in Windows packaging CI.

The final production package (`49247e89…4f6e53`) subsequently passed the actual migration, unattended maintenance and running-uninstall sequence detailed in the linked migration record. Five old policy files remained unchanged; both fixture PATH entries were removed without changing the other 17 tokens. Nine database logical digests and three history/workspace file hashes were equal across uninstall. Original host globals were then restored, and only the unrelated development Node remained running. This resolves both defects for the tested path without converting the earlier failed candidates into passes.
