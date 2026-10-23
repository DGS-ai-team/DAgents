# Standard-user install and recovery audit

Date: 2026-10-20  
Environment: developer machine, using a temporary standard Windows user account. The temporary account name and SID are intentionally omitted.

## Result

The core standard-user lifecycle passed for the isolated QA installer: silent fresh install, first-run setup and Mock conversation, shortcut exit/reopen persistence, silent upgrade, uninstall with user data retained, and reinstall with prior Agent/history restored. The final test uninstall completed.

This was a QA-only `0.11.0.1` installer. Its staged payload and bundle `VERSION` remained `0.11.0`; it is not a release candidate, and this result does not establish acceptance of a frozen release package.

## Artifacts

| Artifact | SHA-256 |
| --- | --- |
| Source bundle manifest | `a6b00f04c701ca04c2e098a8f266eafca6d485d3148bd49bdd58cce8f8741fec` |
| Original x64 installer | `49247e89be04a2b8ffafa1489b22dc72eb192af978b443a6e3f88f411a4f6e53` |
| QA upgrade installer `0.11.0.1` | `2c5ee26a51f47dfeefbf6b3b0076736dd8292e84584d2e6717c7230ed294caa8` |

The package pair exercised `0.11.0 → 0.11.0.1` through the QA AppId and installer path. The application payload itself was still version `0.11.0`, so this verifies installer behavior with that QA pair, not an upgrade from a previous public release.

## Steps and evidence

- Ran a silent fresh install as a non-admin standard user, completed native onboarding, and sent a Mock conversation. No external provider credential was used.
- Exited and reopened Desktop using its shortcut. The configured Agent and conversation state persisted.
- Ran a silent `0.11.0 → 0.11.0.1` upgrade with the existing policy branch present, then reopened Desktop and verified the existing setup and history remained available.
- Created a cold backup containing 45 files totaling 539,406 bytes. All nine SQLite databases passed `PRAGMA integrity_check` with `ok`.
- Ran a silent uninstall. Hashes for the Node runtime/configuration and Desktop settings remained unchanged. The uninstall removed its own PATH entries while preserving unrelated PATH entries and the existing Run key.
- Reinstalled and verified the prior Agent and history were restored. The final test uninstall completed.

## Remaining release checks

This run does not close checks for a missing WebView2 runtime, locked files, unwritable install paths, backup-restore fault injection, preservation when autostart is enabled, or rollback behavior. It did not test a real provider or encrypted external-provider key, SSH, or 24-hour Auto operation. The test was performed on the developer machine under a temporary standard-user account, not on a clean VM; only the QA installer was used. Repeat the core path against the frozen release candidate before release and keep these remaining checks open.

No runtime or product code was changed as part of this audit.
