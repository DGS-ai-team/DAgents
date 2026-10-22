import { inject } from "vue";

export const ResourceRenderContextKey = Symbol.for("dagents.resourceRenderContext");

export function validateWorkspacePath(path) {
  // eslint-disable-next-line no-control-regex -- Workspace paths must reject C0/DEL controls.
  if (typeof path !== "string" || !path || path.includes("\\") || path.startsWith("/") || path.includes(":") || /[\u0000-\u001f\u007f]/.test(path)) return false;
  const segments = path.split("/");
  return !segments.some((segment) => !segment || segment === "." || segment === ".." || segment.toLowerCase() === ".dagents");
}

/** Parse only the documented, local resource URL forms. Values are decoded once. */
export function parseResourceUrl(href) {
  if (typeof href !== "string" || !href.startsWith("dagents://")) return null;
  const match = /^dagents:\/\/resource\/(file|image|terminal)\?([^#?]*)$/.exec(href);
  if (!match) return null;
  const [, type, query] = match;
  const pairs = query.split("&").map((part) => {
    const eq = part.indexOf("=");
    if (eq < 1) return null;
    try { return [decodeURIComponent(part.slice(0, eq)), decodeURIComponent(part.slice(eq + 1))]; }
    catch { return null; }
  });
  if (pairs.some((p) => !p)) return null;
  const key = type === "terminal" ? "id" : "path";
  if (pairs.length !== 1 || pairs[0][0] !== key || !pairs[0][1]) return null;
  const value = pairs[0][1];
  if (type === "terminal") {
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value)) return null;
    return { type, id: value };
  }
  if (!validateWorkspacePath(value)) return null;
  return { type, path: value };
}

export function useResourceRenderContext() {
  return inject(ResourceRenderContextKey, null);
}
