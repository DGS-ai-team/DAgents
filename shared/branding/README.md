# DAgents branding assets

`brand-icon.png` is the only source of truth for the DAgents product mark used by the installer, tray application, Node Web UI, and Manage Console.

The Node and Manage Vite builds expose this directory through the `@dagents-brand` alias. Web UI branding, including the Node favicon, is bundled from that import; do not add a copied image under a frontend `public/` directory or reach into another frontend package.

Windows Shell and installer images under `desktop/tray*/` and `packaging/windows/assets/` are generated derivatives required by native toolchains. Regenerate them with `packaging/windows/scripts/generate-tray-icons.py` and `packaging/windows/scripts/generate-wizard-assets.py` after changing the canonical PNG. Those generated files are build inputs, not independent branding sources, and must not be hand-edited.
