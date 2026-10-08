# Prototype icon usage

`docs/design/desktop-product/icons/` is the only editable icon source for Desktop. `icons/manifest.json` maps every prototype icon to its DAgents or Lucide origin and license; `scripts/sync-icons.mjs` copies that list into the generated `public/assets/icons/` directory before `npm run dev` and `npm run build`.
