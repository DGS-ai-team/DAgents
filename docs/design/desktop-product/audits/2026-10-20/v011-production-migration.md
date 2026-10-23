# Official v0.11.0 to the corrected installer candidate

This record concerns the official September 11 release installer, not a synthetic package-version upgrade. It complements the [production installation audit](browser-chain-and-production-install.md). All data is synthetic, on the user-authorized elevated Windows 11 development machine; standard-user acceptance is a separate gate. Source `VERSION` remains `0.11.0`; no release tag is created by this test.

## Inputs and old installation

The official `v0.11.0` x64 installer is 60,558,443 bytes, SHA-256 `64e3d2f024e66ef9ed1cec862a2958f3afc1067a37c26f375d19a8cc5e03158a`. Release/tag provenance is retained in ignored `.runtime-test-v011-migration-20261020/inputs/`. Its tag commit is `066a5d8996839628097b7f13aa9d0575866f7b02`.

Root ran the reviewed two-phase helper with Windows PowerShell 5.1. The production AppId was absent beforehand. Actual `/CURRENTUSER` installation into the Chinese/space path `旧版 fixture/DAgents 本地安装` returned `0`, created the old Start Menu launchers and Run value, and started exactly the fixture's Shell and Node. Child APPDATA, LOCALAPPDATA, temporary and WebView2 directories were isolated; inherited provider credentials and Node identity overrides were removed. Port `18964` belonged to the expected installed Node, with Node ID `b2c54c64-f6ad-4f22-a85c-800d1622e081`.

Through the old setup API, root created Node preferences and a Mock profile containing a dedicated synthetic Key. Ordinary Agent `agt-d68ca1a0a26c9d46` completed a two-message conversation. Root also wrote a 42-byte `迁移验收.txt` in its private workspace, SHA-256 `42bd74fc4447c41bb5bb7591112cac938fe4baecdfe13a74e0fabfbe14c68828`. No actual provider was called.

## Cold backup

After confirming the Agent idle, the reviewed helper stopped only exact fixture executable/PID matches. Both old Shell and Node required forced termination after the bounded non-force request; this is **not** evidence of graceful application shutdown. The unrelated development Node was preserved.

The full `.runtime` and config were copied to `cold-backup-20261019T234558Z` with long-path-capable Robocopy. All 51 runtime files, 537,789 bytes, matched by relative path, length and SHA-256. The 32-byte `.llm_secret_key` was backed up with the encrypted LLM database; only its hash/length were recorded. Nine databases passed read-only SQLite `integrity_check`. Root then regenerated the entire backup manifest after that check and again confirmed exact equality with the frozen source manifest. Both source data and verified backup remain retained.

## PATH-corrected candidate

The candidate reuses the 51-file, 412,343,879-byte payload bundle from source `6a0107c2694da7cbe2571813f7da628f04ba431c`, changing only the production installer source described in the companion audit. Root independently verified every staged bundle file, installer hash and patched-source hash.

- Patched installer source SHA-256: `48f5c10228c8fe30117034f19b102c2915a1cf0c988e74ad833685fbdeea911f`.
- Inno Setup 6.7.3 compiler SHA-256: `0a8757031b33777e4c9cbffee40f11a5062b36d25cbe144c1db73b6102b80ad7`.
- Production installer SHA-256: `09f7cadc41628784de5ee0c064f4ad2afc6f4b47264796041d3368cff5564489`.

Ignored build provenance is in `.runtime-test-path-cleanup-production-v2-20261020/`; the prior failed package and intermediate build have not been overwritten.

## Old uninstall and restore staging

Root ran the actual old uninstaller only after checking its production registration, exact install path, absent fixture processes/listener, verified backup, unchanged global state, and the absence of any other Shell that the old broad stop helper could affect. Uninstall returned `0`; its AppId disappeared from all four hive/view checks and its launchers/Run value were removed. The old installer does not remove its PATH additions, so the QA helper restored the original complete value and `String` registry kind only after an exact full-value match to the known installation delta. This guarded cleanup is not attributed to the old product.

The real Desktop settings hash and the unrelated `18977` development Node ID stayed unchanged. Evidence is `preinstall-state/uninstall-restore-result.json`; both the frozen backup and old retained data remain available.

The new production helper then passed preflight and created a fresh Chinese/space target under `.runtime-test-production-pathfix-20261020/`. Before installation, root restored the full old `.runtime` into that target and verified all 51 files again against the cold-backup manifest. The new bootstrap config is listen-only at `127.0.0.1:18964`; the old YAML `local` block is not copied. No old application binaries or ownership marker were copied into the new target.

## Existing-policy silent-install failure

That real production setup stalled before installing files: despite `/VERYSILENT /SUPPRESSMSGBOXES`, `NextButtonClick` used ordinary `MsgBox` to ask whether to overwrite the restored policy. The log records the waiting Yes/No message. Root stopped only that installer parent/child pair and retained the failure log; its `-1` exit was a QA termination, not a product success.

Independent postconditions confirmed no AppId in any hive/view, unchanged original PATH value/type, Run and all tracked shortcuts, and no installed `bin` directory. The staged runtime and frozen backup were preserved. This is a second installer defect found by actual old-data recovery; it requires a policy-preserving silent-mode correction and a new candidate, rather than a weakened unattended-install assertion.

## Final production package and successful recovery

The final installer combines both corrections. Source SHA-256 is `305df1f071bad08eef8cdfcf1c630bea77ce390cd6c7ef28be505a776e71cf91`; installer SHA-256 is `49247e89be04a2b8ffafa1489b22dc72eb192af978b443a6e3f88f411a4f6e53` (390,818,415 bytes). Inputs remain the same verified application bundle and Inno compiler. Build provenance is retained in `.runtime-test-path-cleanup-production-v3-20261020/`; operational evidence is under `.runtime-test-production-policyfix-v3-20261020/`.

Root restored the 51-file cold runtime into a second fresh Chinese/space installation target, with exact manifest equality before setup. Actual silent production installation completed with exit `0` and launched its Desktop/owned Node. The helper verified all four installed executable hashes, default-off desktop icon and autostart, the actual Start Menu link, and the exact two-path installation delta. Existing policy did not block setup.

The running Node retained ID `b2c54c64-f6ad-4f22-a85c-800d1622e081`. The entire original two-message transcript compared equal through the current hydrate API; Agent identity, model profile ID, stored-Key presence, and the Chinese workspace file/hash were retained. Root inspected the real Tauri window, which directly displayed the migrated Agent and original conversation without Node onboarding.

To verify actual decryption, root edited the existing synthetic profile to use a loopback OpenAI-compatible server on `18965`, deliberately omitting `api_key` from the edit. The server accepted exactly one streaming completion request with Authorization matching the original synthetic Key, and the Agent completed with `migration-stub-ok`. The server records only match/stream/count information, not headers or request bodies. This proves paired-key recovery and omission-preserving profile editing for this fixture; it does not validate an actual provider account.

That settings save canonicalized the listen-only bootstrap config. Root checked the complete canonical bytes and hash (`c0b033e6…e541197`), unchanged listener and Node ID, retained the original seed hash, and recorded a separate one-shot expected-config update. This was a known settings-save side effect, not data corruption or a relaxed PATH assertion.

Root then ran actual maintenance installation with desktop icon and autostart enabled. It completed unattended with exit `0`; five existing policy files remained byte-identical, all four transcript items survived, and the exact Run command/shortcuts were verified. The native Desktop displayed both the old conversation and new successful response, with the updated model label.

## Running uninstall and final host cleanup

With the Agent idle and Node/Desktop running, root ran the real final uninstaller. It returned `0`, stopped only the installation's owned processes, removed its AppId/shortcuts/autostart, and removed both fixture PATH entries. The other 17 PATH entries retained their exact values and order. The product leaves PATH as `ExpandString`; after checking that postcondition, the QA helper restored the original `String` kind and verified the full original value. This registry-kind restoration belongs to QA cleanup, not a claim that the product remembers the prior registry kind.

Before/after uninstall snapshots contain nine SQLite databases and three history/workspace-scope files. Their complete snapshot files have identical SHA-256 `1c200f4d7a172292367b1e007e3ebe3b5a568ad58fd20f2e143edadc627ede12`. This compares logical database schema/rows and file contents rather than WAL-dependent physical database bytes. An initial summary read a nonexistent digest property; root replaced that comparison with independently verified full-file hashes and retained the preliminary summary as helper evidence.

The original Run/PATH/shortcut state and real Desktop settings hash were restored and verified. The local provider stub was stopped; no test Desktop/Node remains running. The unrelated `18977` Node retained its original identity. Synthetic data and cold backups remain in the ignored QA directories.

This closes the **tested administrator-machine ordinary-Agent migration path**, including an encrypted synthetic Key, private workspace, maintenance and data-preserving uninstall. It does not establish standard-user installation, external custom workspace restoration, populated Auto/pending-trigger recovery, actual provider accounts, missing WebView2, fresh-login autostart execution, or failure rollback to the old executable. Those release gates remain open; the prior Go fixture evidence for pending-trigger recovery is distinct from this installer run.
