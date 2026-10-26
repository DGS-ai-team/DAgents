# DAgents branding assets

`brand-icon.png` is the source of truth for the product mark used by Desktop, the installer, and Manage Console. Node only provides runtime APIs and does not publish a Web UI.

The shared Vue sources and Manage Vite build expose this directory through the `@dagents-brand` alias. UI branding is bundled from that import; do not copy it into frontend `public/` directories or another package. SVG UI assets are separately managed in `shared/icons`; the product mark remains the original PNG.

Windows Desktop and installer images under `desktop/tray-tauri/` and `packaging/windows/assets/` are generated derivatives required by native toolchains. Regenerate them with `packaging/windows/scripts/generate-tray-icons.py` and `packaging/windows/scripts/generate-wizard-assets.py` after changing the canonical PNG. Those generated files are build inputs, not independent branding sources, and must not be hand-edited.
