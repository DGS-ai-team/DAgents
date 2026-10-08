import { copyFile, mkdir, readFile, readdir, unlink } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const source = resolve(productRoot, "icons");
const target = resolve(productRoot, "prototype/public/assets/icons");
const expectedTarget = resolve(productRoot, "prototype", "public", "assets", "icons");
if (target !== expectedTarget) throw new Error(`Refusing to clean unexpected icon directory: ${target}`);

const manifest = JSON.parse(await readFile(resolve(source, "manifest.json"), "utf8"));
if (!Array.isArray(manifest.icons) || manifest.icons.length === 0) throw new Error("Desktop icon manifest is empty or invalid.");
const files = manifest.icons.map(({ file }) => {
  if (typeof file !== "string" || !/^[a-z0-9-]+\.svg$/.test(file)) throw new Error(`Invalid icon filename in manifest: ${file}`);
  return file;
});
if (new Set(files).size !== files.length) throw new Error("Duplicate icon filenames in Desktop icon manifest.");

await mkdir(target, { recursive: true });
for (const file of files) await copyFile(resolve(source, file), resolve(target, file));
const expectedNames = new Set(files);
// Cleanup is intentionally limited to the confirmed generated prototype icon directory.
for (const name of await readdir(target)) {
  if (name.endsWith(".svg") && !expectedNames.has(name)) await unlink(resolve(target, name));
}
console.log(`Synced ${files.length} Desktop icons and removed stale SVGs from prototype/public/assets/icons.`);
