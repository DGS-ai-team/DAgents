# Resource references in assistant messages

Assistant messages may refer to resources owned by the current Agent with standard Markdown links:

```md
[报告](dagents://resource/file?path=reports%2Fsummary.md)
[终端](dagents://resource/terminal?id=local-terminal-5)
![图片](dagents://resource/image?path=images%2Fplot.png)
```

The URI authority is exactly `resource`; supported paths are `/file`, `/terminal`, and `/image`. Files and images use one `path` query parameter containing a percent-encoded, Agent-workspace-relative path. Terminal links use one `id` parameter containing the terminal's registered ID. Values are decoded once. Absolute paths, drive paths, UNC paths, traversal, empty or repeated path components, `.dagents`, colons, control characters, extra parameters, fragments, malformed escapes, and unknown resource types are rejected. A percent sequence remaining after decoding is a literal filename character; consumers must not decode it again. The runtime must also confirm that the Agent owns the requested file or terminal before opening it.

Resource rendering is explicitly enabled for assistant message bubbles. Ordinary Markdown renderers keep these URLs inert. Resource URIs inside code blocks, inline code, raw HTML, user messages, and messages without a known Agent owner do not activate references. Workgroup messages without a reliable Agent owner do not support resource activation. Invalid or unknown `dagents:` references fall back to plain text. Images are read through the authenticated Node binary channel (16 MiB per image API limit), then displayed inline; supported formats are PNG, JPEG, GIF, WebP, and SVG. SVG support uses the existing `dagents://resource/image` URI; the frontend renders it as an image surface without inserting SVG markup into the host DOM. The UI does not fetch localhost or use `file://` URLs. At most eight images are loaded per message. File and terminal references open through the host's resource resolver.

## Explicit HTML and SVG display blocks

When ordinary Markdown cannot clearly express the result, assistant messages may opt into a static HTML or SVG presentation with a standalone display wrapper:

```html
<dagents-render type="html">
<section><h3>本周进展</h3><p>已完成三个事项。</p></section>
</dagents-render>
```

```html
<dagents-render type="svg">
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 300 100">
  <rect x="10" y="10" width="280" height="80" rx="12" fill="#eee"/>
</svg>
</dagents-render>
```

These fences illustrate the protocol. Actual display wrappers appear directly in assistant Markdown, outside code fences and inline code. The opening and closing tags must each occupy their own line. The opening tag has only a quoted `type` attribute with value `html` or `svg`; extra attributes are not accepted. Only a complete standalone wrapper with a supported type activates rendering. Wrappers are not nested. Unsupported or malformed wrappers and incomplete streamed blocks remain visible as ordinary escaped content; closing a valid block enables its preview. Ordinary raw HTML/SVG outside a display wrapper is escaped, and ordinary `html`/`svg` code fences use the shared code renderer without activating previews. There is no automatic raw-HTML or code-fence preview fallback.

HTML and SVG display blocks both initially show the preview. A lightweight toolbar switches between preview and source, displaying one view at a time; source uses the shared code styling, highlighting and copy behavior. Copy always copies the original inner source without the display wrapper. Multiple blocks switch independently, and a selection survives continued text updates in the same message. The preview container adds no border or card background; the content defines its own appearance. SVG previews retain the image lightbox. The same behavior applies in the full conversation and compact message card.

User/system messages do not activate display wrappers. Workgroup assistant text can show isolated previews without a known Agent owner; this does not enable its resource links. Display wrappers grant no tool, workspace, Node or native-bridge access and require no resource URI. Workspace SVG images continue to use the resource-image protocol above.

Each message is limited to four display previews in addition to the existing eight resource images. HTML source is limited to 64 Ki characters; SVG source is limited to 256 Ki characters, 4,096 descendant elements, and 64 levels of nesting. SVG supports basic shapes, text, local gradients, markers and clipping; external references, scripts, `foreignObject`, animation and recursive `use` are excluded. A sanitized SVG embedded in an HTML preview becomes an image within the isolated frame.

HTML previews must be isolated from the host application: model-provided scripts do not run, remote/network resources are blocked, the Node/native bridge and local files are inaccessible, and links cannot navigate the host or open popups. The application may inject a fixed measurement bootstrap, authorized by its exact CSP script hash, into an opaque sandbox with `allow-scripts` and without `allow-same-origin`. This bootstrap only reports content dimensions; it does not execute model code or provide a tool/native bridge. The parent accepts size messages only from the registered iframe window, with the current random channel, valid finite dimensions and an active lifetime. Dimensions are bounded before changing the iframe size; old or forged messages are ignored.

HTML preview height follows measured content, with a 24px minimum. Long content expands in the conversation and compact message card; vertical scrolling belongs to the outer message container rather than a second vertical scrollbar inside the preview. Ordinary long words wrap to the available width. Explicit preformatted or wide content may still scroll horizontally; its scrollbar height is included in the measurement so it does not introduce an extra vertical scrollbar. The compact card itself remains bounded so its input stays in place. Pathological layouts exceeding 1,000,000px are replaced with an explicit preview status, while the source toggle remains available; they are not silently clipped. Width changes, image loading and switching back to preview trigger size checks. While the same message continues streaming, unchanged display blocks retain their measured height rather than repeatedly collapsing to the initial height. This height reuse is limited to the current message and is cleared when the message or Agent changes. Scripts and listeners are reclaimed when the preview is replaced or unmounted. This is presentation behavior and adds no parameters to the display-wrapper protocol. SVG remains an image sized from its own dimensions, never inserted as active markup into the host DOM; scripts, foreign content, and external resources must not gain execution or host access. Model instructions should not rely on script execution, external loading, or preview interaction to complete the task.

This protocol requires the Node and Desktop parts of a release to be updated together. Existing `show_image` tool results remain available for historical playback.

The initial resource set is intentionally small. To add a resource type, first define its URI and ownership rules here, then register a strict parser and a host resolver. Never interpret arbitrary URIs or execute content from a model response. Preserve ordinary Markdown behavior and keep sanitization enabled.
