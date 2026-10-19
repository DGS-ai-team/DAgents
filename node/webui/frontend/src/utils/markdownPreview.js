import DOMPurifyModule from "dompurify";

const MAX_HTML_PREVIEW_CHARS = 64 * 1024;
const MAX_SVG_PREVIEW_CHARS = 256 * 1024;
const MAX_SVG_NODES = 4096;
const MAX_SVG_DEPTH = 64;
export const HTML_PREVIEW_MIN_HEIGHT = 24;
export const HTML_PREVIEW_LAYOUT_LIMIT = 1_000_000;
const HTML_MEASURE_BOOTSTRAP = `(function(){'use strict';var s=document.currentScript,c=s&&s.dataset.channel,b=s&&document.getElementById(s.dataset.content),l=-1,r=0;function measure(){r=0;if(!b)return;var d=document.documentElement,scrollbarHeight=Math.max(0,window.innerHeight-d.clientHeight),h=Math.ceil(Math.max(b.getBoundingClientRect().height,b.scrollHeight)+scrollbarHeight);if(!Number.isFinite(h))return;h=Math.max(24,h);if(h!==l){l=h;parent.postMessage({kind:'dagents-preview-height',channel:c,height:h},'*')}}function schedule(){if(!r)r=requestAnimationFrame(measure)}window.addEventListener('message',function(e){var d=e.data;if(e.source===parent&&d&&d.kind==='dagents-preview-measure'&&d.channel===c)schedule()});if(b)new ResizeObserver(schedule).observe(b);window.addEventListener('load',schedule);document.addEventListener('load',schedule,true);schedule()})();`;
const HTML_MEASURE_CSP_HASH = "'sha256-3ARxkojORqNKirpclGIJNQXtwPiGjaD7H8YbXRbY1hU='";
const purifier = typeof DOMPurifyModule === "function"
  ? typeof window !== "undefined" ? DOMPurifyModule(window) : null
  : DOMPurifyModule;

const SVG_ELEMENTS = new Set([
  "svg", "g", "defs", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon", "text", "tspan", "title", "desc",
  "lineargradient", "radialgradient", "stop", "clippath", "marker",
]);
const SVG_ATTRIBUTES = new Set([
  "xmlns", "viewBox", "width", "height", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry",
  "d", "points", "fill", "fill-rule", "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-dasharray",
  "stroke-dashoffset", "opacity", "fill-opacity", "stroke-opacity", "transform", "font-family", "font-size", "font-weight",
  "text-anchor", "dominant-baseline", "preserveAspectRatio", "id", "offset", "stop-color", "stop-opacity",
  "clip-path", "clipPathUnits", "marker-start", "marker-mid", "marker-end", "markerWidth", "markerHeight", "markerUnits",
  "refX", "refY", "orient", "gradientUnits", "gradientTransform", "spreadMethod", "fx", "fy",
]);

function safeSvgAttribute(name, value, { root = false } = {}) {
  // eslint-disable-next-line no-control-regex -- SVG attribute values must reject C0/DEL controls.
  if (!SVG_ATTRIBUTES.has(name) || /[\u0000-\u001f\u007f]/.test(value)) return false;
  if (name === "xmlns") return root && value === "http://www.w3.org/2000/svg";
  if (name === "id") return /^[A-Za-z_][A-Za-z0-9_.-]{0,63}$/.test(value);
  if (["fill", "stroke"].includes(name) && /^url\(#[A-Za-z_][A-Za-z0-9_.-]{0,63}\)$/i.test(value.trim())) return true;
  if (["fill", "stroke", "stop-color"].includes(name)) return /^(?:none|currentcolor|transparent|black|white|red|green|blue|gray|grey|yellow|orange|purple|pink|brown|navy|teal|aqua|fuchsia|lime|maroon|olive|silver|#[\da-f]{3,8}|rgba?\([\d.%\s,]+\)|hsla?\([\d.%\s,]+\))$/i.test(value.trim());
  if (["clip-path", "mask", "marker-start", "marker-mid", "marker-end"].includes(name)) return value === "none" || /^url\(#[A-Za-z_][A-Za-z0-9_.-]{0,63}\)$/.test(value.trim());
  if (name === "font-family") return /^[\w\s,'"-]{1,80}$/.test(value);
  if (["transform", "gradientTransform"].includes(name)) return /^(?:(?:matrix|translate|scale|rotate|skewX|skewY)\([\d.,+\-\s]+\)\s*)+$/i.test(value);
  if (["d", "points"].includes(name)) return /^[MmZzLlHhVvCcSsQqTtAaEeFf0-9.,+\-\s]*$/.test(value);
  if (["viewBox", "stroke-dasharray"].includes(name)) return value === "none" || /^[0-9.,+\-\s]+$/.test(value);
  if (name === "preserveAspectRatio") return /^(?:none|x(?:Min|Mid|Max)Y(?:Min|Mid|Max)(?:\s+(?:meet|slice))?)$/i.test(value);
  if (["width", "height"].includes(name)) return /^(?:\d{1,5}(?:\.\d+)?(?:px|%)?|auto)$/.test(value);
  if (["opacity", "fill-opacity", "stroke-opacity"].includes(name)) return /^(?:0(?:\.\d+)?|1(?:\.0+)?)$/.test(value);
  if (["font-size", "stroke-width", "stroke-dashoffset", "markerWidth", "markerHeight", "refX", "refY", "fx", "fy"].includes(name)) return /^-?\d{1,5}(?:\.\d+)?(?:px|em|%)?$/.test(value);
  if (name === "font-weight") return /^(?:normal|bold|[1-9]00)$/.test(value);
  if (["text-anchor", "dominant-baseline", "fill-rule", "stroke-linecap", "stroke-linejoin", "spreadMethod", "gradientUnits", "clipPathUnits", "maskUnits", "markerUnits"].includes(name)) return /^[a-z-]{1,24}$/i.test(value);
  if (name === "orient") return /^(?:auto|auto-start-reverse|-?\d{1,3}(?:\.\d+)?(?:deg)?)$/i.test(value);
  if (name === "offset") return /^(?:0|1|0?\.\d+|100%|\d{1,2}(?:\.\d+)?%)$/.test(value);
  if (["x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry"].includes(name)) return /^-?\d{1,6}(?:\.\d+)?(?:px|%)?$/.test(value);
  if (name === "xmlns") return value === "http://www.w3.org/2000/svg";
  return false;
}

export function sanitizeSvg(source) {
  const text = String(source || "");
  if (!text || text.length > MAX_SVG_PREVIEW_CHARS || /<!DOCTYPE|<!ENTITY/i.test(text) || typeof DOMParser === "undefined") return null;
  const parsed = new DOMParser().parseFromString(text, "image/svg+xml");
  if (parsed.querySelector("parsererror")) return null;
  const root = parsed.documentElement;
  if (!root || root.localName.toLowerCase() !== "svg" || (root.namespaceURI && root.namespaceURI !== "http://www.w3.org/2000/svg")) return null;
  const allElements = [...root.querySelectorAll("*")];
  if (allElements.length > MAX_SVG_NODES) return null;
  const depths = [{ node: root, depth: 1 }];
  while (depths.length) {
    const { node, depth } = depths.pop();
    if (depth > MAX_SVG_DEPTH) return null;
    for (const child of node.children) depths.push({ node: child, depth: depth + 1 });
  }
  const visit = (node) => {
    for (const child of [...node.children]) {
      if (!SVG_ELEMENTS.has(child.localName.toLowerCase()) || (child.namespaceURI && child.namespaceURI !== "http://www.w3.org/2000/svg")) {
        child.remove();
        continue;
      }
      for (const attribute of [...child.attributes]) {
        const name = attribute.name;
        const value = attribute.value;
        if (!safeSvgAttribute(name, value)) child.removeAttribute(name);
      }
      visit(child);
    }
  };
  for (const attribute of [...root.attributes]) {
    if (!safeSvgAttribute(attribute.name, attribute.value, { root: true })) root.removeAttribute(attribute.name);
  }
  if (!root.hasAttribute("xmlns")) root.setAttribute("xmlns", "http://www.w3.org/2000/svg");
  visit(root);
  const ids = new Set();
  for (const element of [root, ...root.querySelectorAll("[id]")]) {
    const id = element.getAttribute("id");
    if (!id) continue;
    if (ids.has(id)) element.removeAttribute("id");
    else ids.add(id);
  }
  for (const element of [root, ...root.querySelectorAll("*")]) {
    for (const attribute of [...element.attributes]) {
      const match = /^(?:url\(#([A-Za-z_][A-Za-z0-9_.-]{0,63})\))$/.exec(attribute.value.trim());
      const referenceId = match?.[1];
      if (referenceId && !ids.has(referenceId)) element.removeAttribute(attribute.name);
    }
  }
  return new XMLSerializer().serializeToString(root);
}

export function sanitizeHtmlPreview(source) {
  const text = String(source || "");
  if (!text || text.length > MAX_HTML_PREVIEW_CHARS || !purifier) return "";
  // Markdown HTML cards may contain static inline SVG charts. Sanitize each
  // SVG independently and convert it to a data image before DOMPurify runs;
  // SVG never enters the iframe DOM as an active element.
  const parsedSource = new DOMParser().parseFromString(text, "text/html");
  for (const svg of [...parsedSource.querySelectorAll("svg")]) {
    const safeSvg = sanitizeSvg(new XMLSerializer().serializeToString(svg));
    if (!safeSvg) {
      svg.remove();
      continue;
    }
    const image = document.createElement("img");
    image.setAttribute("src", `data:image/svg+xml;base64,${btoa(unescape(encodeURIComponent(safeSvg)))}`);
    image.setAttribute("alt", "SVG 图表");
    svg.replaceWith(image);
  }
  const preparedText = parsedSource.body.innerHTML;
  const fragment = purifier.sanitize(preparedText, {
    RETURN_DOM_FRAGMENT: true,
    FORBID_TAGS: ["script", "iframe", "frame", "object", "embed", "form", "meta", "link", "base", "svg", "math", "video", "audio", "source"],
    FORBID_ATTR: ["srcset", "action", "formaction", "target"],
  });
  const nodes = [...fragment.children, ...fragment.querySelectorAll("*")];
  for (const node of nodes) {
    if (node.localName === "style" && /url\s*\(|@import|expression\s*\(|javascript:/i.test(node.textContent || "")) {
      node.remove();
      continue;
    }
    node.removeAttribute("href");
    for (const attribute of [...node.attributes]) {
      if (/^on/i.test(attribute.name)) node.removeAttribute(attribute.name);
    }
    if (node.hasAttribute("src")) {
      const src = node.getAttribute("src") || "";
      const safeRaster = node.localName === "img" && /^data:image\/(?:png|jpeg|gif|webp|svg\+xml);base64,[a-z0-9+/=]+$/i.test(src) && src.length <= 1_500_000;
      if (!safeRaster) node.removeAttribute("src");
    }
    if (node.hasAttribute("style") && /url\s*\(|@import|expression\s*\(|javascript:/i.test(node.getAttribute("style") || "")) node.removeAttribute("style");
  }
  const wrapper = document.createElement("div");
  wrapper.append(fragment);
  return wrapper.innerHTML;
}

export function createHtmlPreviewDocument(source, channel = "") {
  const safeHtml = sanitizeHtmlPreview(source);
  if (!safeHtml) return "";
  if (!/^[a-f0-9]{32}$/.test(channel)) return "";
  const contentId = `dp-${channel}`;
  const csp = `default-src 'none'; script-src ${HTML_MEASURE_CSP_HASH}; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; navigate-to 'none'; img-src data: blob:; style-src 'unsafe-inline'; font-src 'none'; media-src 'none'`;
  const layoutCss = `html:root,html:root body{height:auto!important;min-height:0!important;max-height:none!important;overflow:visible!important}html:root body{width:100%!important;margin:0!important;overflow-wrap:anywhere!important;word-break:break-word!important}html:root body #${contentId}{display:flow-root!important;width:100%!important;max-width:100%!important;height:auto!important;min-height:0!important;max-height:none!important;overflow:visible!important;overflow-wrap:anywhere!important;word-break:break-word!important;box-sizing:border-box!important}`;
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${csp}"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0"><div id="${contentId}" style="display:flow-root;width:100%">${safeHtml}</div><style>${layoutCss}</style><script data-channel="${channel}" data-content="${contentId}">${HTML_MEASURE_BOOTSTRAP}</script></body></html>`;
}

export const MAX_HTML_PREVIEW_CHARS_LIMIT = MAX_HTML_PREVIEW_CHARS;
export const MAX_SVG_PREVIEW_CHARS_LIMIT = MAX_SVG_PREVIEW_CHARS;
