# DAgents branding assets

`brand-icon.png` is the canonical DAgents product mark used by the installer, tray application, Node Web UI, and Manage Console.

The Node and Manage Vite builds expose this directory through the `@dagents-brand` alias. New web UI branding should import the asset through that alias instead of copying the file or reaching into another frontend package.
