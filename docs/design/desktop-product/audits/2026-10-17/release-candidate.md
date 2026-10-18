# 2026-10-17 x64 release candidate build record

This records a **non-release** Windows x64 candidate for development acceptance version `0.11.0`. The ignored build area is `.runtime-test-release-closeout-20261017/`; final binaries, assembled bundle, candidate installers, and a per-file JSON manifest are stored there. The source was built from product commit `29fc308e766826950ff51f8b0e8ec4eb8473c7a3` (release platform/update support) after `00a4658cfa5ad2ddaf5a5c0e56c5b65460aaad56` removed unused vulnerable Rust `zip`; checkout HEAD during candidate build was `12738e4aeaad6b1528d483f1d6df75fb02fbd60b`, which added documentation only. No product source changed after the product commit. CI run [39011353761](https://github.com/DGS-ai-team/DAgents/actions/runs/39011353761) passed, including the online RustSec audit.

The release and manual-package workflows now produce only the complete Windows x64 package. The Windows 386 Go cross-build remains available in development CI, but no x86 full package is emitted. The installer/update path uses exact platform assets and reports when a platform has no published package; it does not substitute x64 for x86.

## Built inputs

Node and Client were cross-compiled with Go `1.26.9`, `GOOS=windows`, `GOARCH=amd64`, and `CGO_ENABLED=0`, using package version `0.11.0`. `go version -m` confirmed both compiler and target metadata. Their PE machine is `0x8664`:

- `dagents-node.exe`: 19,175,936 bytes; SHA-256 `69fc2b6d905b57b9a8e0b4c37b0dc8ff432d18368e3f2ab4d093c5da3a6a5c85`.
- `dagents-client.exe`: 7,008,768 bytes; SHA-256 `99339b14c3359108db753d618ac9a48351417c52c1c3e84b72067f0ca23ddb03`.

Tauri Desktop was rebuilt locally as an x64 MSVC release using the current source and lockfile. Vite completed successfully; Cargo completed the release build. The executable was checked as PE machine `0x8664`, FileVersion/ProductVersion `0.11.0`, 4,786,688 bytes, SHA-256 `90fa6dda3e49e8d8e6256632f8fb309a6587de299232f8367cf96502b84533c4`. This replaces the earlier downloaded Desktop artifact that predated the Rust dependency cleanup.

The x64 Browser input is the isolated PyInstaller build from Python `3.13.14` x64, Playwright `1.63.0`, PyInstaller `6.22.3`, Chromium `153.0.8010.12`; it includes Chromium headless shell. The executable is 381,288,263 bytes, PE machine `0x8664`, SHA-256 `ae97ea25ec3c27b47a9784c076a4e001dfb58411a9d17dc34a5836963d6a2b7a`. Browser payload size was 739,894,015 bytes. No browser service or Chromium executable was launched.

The assembled x64 bundle contains 51 files totaling 412,346,366 bytes; the sorted per-file path/size/hash manifest digest is `1bd41294a0eb0080a50d64ad71e4c2186458136e09fa4bd54a7a6b2170c094d6`. It contains the 24-file `packaging/runtime` seed (file-list digest `0de214463464f920e5f461cc1ac5f9ddd06796a496d4341281dc146a28a27999`).

## Inno compile results

Both installers compiled successfully with isolated portable Inno Setup `6.7.3` (`ISCC.exe` SHA-256 `0a8757031b33777e4c9cbffee40f11a5062b36d25cbe144c1db73b6102b80ad7`):

- QA installer: 390,814,965 bytes; SHA-256 `12dbbfc3bbdb8b1a121d30e41541fc556f718d21a690342a7004612037894325`.
- Production installer: 390,816,531 bytes; SHA-256 `d41866416bd25bc86b46cf6422e274e9be6553ddf50cd81cefc1e05f29b4d1da`.

Both candidate files were copied from this compile into `candidate/installers/`. They have been hash-checked against the compiler outputs. The installers were compiled only; neither installer was launched.

The root reviewer independently verified every bundle file against the 51-file manifest and all six binary/installer records for size and SHA-256. A separate checksum file is retained at `.runtime-test-release-closeout-20261017/SHA256SUMS`; binaries and generated checksums are intentionally untracked.

## x86 boundary and execution scope

The user selected a Windows x64-only complete package for this release and paused the x86 complete package. Independently, the current Playwright `1.63.0` win32 wheel embeds an x64 `node.exe` and its runtime maps the win32 platform to `win64`; the pinned `greenlet==3.5.5` also has no win32 wheel. There is no valid x86 Browser payload, and no x64 binary was copied into an x86 candidate. Evidence and wheel files are retained under `.runtime-test-release-closeout-20261017/evidence/`.

No Node application, Desktop app, Browser service, Chromium, or installer was launched. The only runtime-related activity was build tooling and read-only metadata/hash inspection. This candidate is not a release artifact; no version, tag, or release was created. Exact bundle, binary, and installer file hashes are in `.runtime-test-release-closeout-20261017/candidate-manifest.json`.
