function isBlank(line) {
  return /^[\t ]*$/.test(line);
}

function classifyBlock(line) {
  if (/^ {0,3}>/.test(line)) return "quote";
  if (/^ {0,3}(?:[-+*]|\d+[.)])[\t ]+/.test(line)) return "list";
  if (/^(?: {4,}|\t)\S/.test(line)) return "indented";
  if (/^ {0,3}(?:`{3,}|~{3,})/.test(line)) return "fence";
  if (/^ {0,3}<dagents-render\b[^>]*>[\t ]*$/i.test(line)) return "render";
  return "paragraph";
}

function isComplexContinuation(kind, line) {
  if (kind === "quote") return /^ {0,3}>/.test(line);
  if (kind === "list") return /^(?: {2,}|\t)\S/.test(line) || /^ {0,3}(?:[-+*]|\d+[.)])[\t ]+/.test(line);
  if (kind === "indented") return /^(?: {4,}|\t)\S/.test(line);
  if (kind === "complex") return /^ {0,3}(?:>|(?:[-+*]|\d+[.)])[\t ]+)/.test(line) || /^(?: {2,}|\t)\S/.test(line);
  return false;
}

function isAmbiguousContinuationPrefix(scanner, line) {
  if (scanner.inlineCodeLength || /^[\t ]*$/.test(line)) return true;
  if (["list", "complex"].includes(scanner.blockKind) && /^ {0,3}(?:[-+*]|\d+[.)])?$/.test(line)) return true;
  if (["indented", "complex"].includes(scanner.blockKind) && /^[\t ]+$/.test(line)) return true;
  return isComplexContinuation(scanner.blockKind, line);
}

function fenceOpening(line) {
  return /^ {0,3}(`{3,}|~{3,})/.exec(line)?.[1] || "";
}

function fenceClosing(line, fence) {
  if (!fence) return false;
  const marker = fence[0] === "`" ? "`" : "~";
  return new RegExp(`^ {0,3}${marker}{${fence.length},}[\\t ]*$`).test(line);
}

function updateInlineCode(state, line) {
  for (let index = 0; index < line.length;) {
    if (line[index] !== "`" || isEscaped(line, index)) {
      index += 1;
      continue;
    }
    let end = index + 1;
    while (line[end] === "`") end += 1;
    const length = end - index;
    if (!state.inlineCodeLength) state.inlineCodeLength = length;
    else if (state.inlineCodeLength === length) state.inlineCodeLength = 0;
    index = end;
  }
}

function isEscaped(text, index) {
  let slashes = 0;
  for (let cursor = index - 1; cursor >= 0 && text[cursor] === "\\"; cursor -= 1) slashes += 1;
  return slashes % 2 === 1;
}

export function createMarkdownBoundaryScanner() {
  return {
    source: "",
    scanOffset: 0,
    stableBoundary: 0,
    stableRevision: 0,
    pendingBlankBoundary: 0,
    blockKind: "",
    fence: "",
    renderDepth: 0,
    inlineCodeLength: 0,
  };
}

function setStableBoundary(scanner, boundary) {
  if (boundary <= scanner.stableBoundary) return;
  scanner.stableBoundary = boundary;
  scanner.stableRevision += 1;
}

function processLine(scanner, line, lineEnd) {
  if (scanner.fence) {
    if (fenceClosing(line, scanner.fence)) {
      scanner.fence = "";
      if (scanner.blockKind === "fence") {
        setStableBoundary(scanner, lineEnd);
        scanner.blockKind = "";
      }
    }
    return;
  }

  if (scanner.renderDepth > 0) {
    scanner.fence = fenceOpening(line);
    if (scanner.fence) return;
    if (/^ {0,3}<dagents-render\b[^>]*>[\t ]*$/i.test(line)) scanner.renderDepth += 1;
    else if (/^ {0,3}<\/dagents-render>[\t ]*$/i.test(line)) {
      scanner.renderDepth -= 1;
      if (scanner.renderDepth === 0) {
        setStableBoundary(scanner, lineEnd);
        scanner.blockKind = "";
      }
    }
    return;
  }

  if (isBlank(line)) {
    if (scanner.inlineCodeLength || ["list", "quote", "indented", "complex"].includes(scanner.blockKind)) {
      scanner.pendingBlankBoundary = lineEnd;
    } else {
      setStableBoundary(scanner, lineEnd);
      scanner.blockKind = "";
      scanner.inlineCodeLength = 0;
    }
    return;
  }

  if (scanner.pendingBlankBoundary) {
    if (scanner.inlineCodeLength) {
      // Inline code spans can include line breaks; keep the entire span together.
      scanner.pendingBlankBoundary = 0;
    } else if (isComplexContinuation(scanner.blockKind, line)) {
      scanner.pendingBlankBoundary = 0;
    } else {
      setStableBoundary(scanner, scanner.pendingBlankBoundary);
      scanner.blockKind = "";
      scanner.inlineCodeLength = 0;
      scanner.pendingBlankBoundary = 0;
    }
  }

  const nextKind = classifyBlock(line);
  if (!scanner.blockKind) scanner.blockKind = nextKind;
  else if (scanner.blockKind === "paragraph" && nextKind !== "paragraph") scanner.blockKind = "complex";

  const fence = fenceOpening(line);
  if (fence) {
    scanner.fence = fence;
    if (scanner.blockKind !== "complex") scanner.blockKind = "fence";
    return;
  }
  if (/^ {0,3}<dagents-render\b[^>]*>[\t ]*$/i.test(line)) {
    scanner.renderDepth = 1;
    if (scanner.blockKind !== "complex") scanner.blockKind = "render";
    return;
  }

  updateInlineCode(scanner, line);
}

/** Advance only newly completed lines and return the last safe Markdown block boundary. */
export function advanceMarkdownBoundaryScanner(scanner, source, { appendOnly = false } = {}) {
  const text = String(source || "").replace(/\r\n?/g, "\n");
  let reset = false;
  if (text.length < scanner.source.length || (!appendOnly && !text.startsWith(scanner.source))) {
    Object.assign(scanner, createMarkdownBoundaryScanner());
    reset = true;
  }
  scanner.source = text;
  for (;;) {
    const newline = text.indexOf("\n", scanner.scanOffset);
    if (newline < 0) break;
    const line = text.slice(scanner.scanOffset, newline);
    const lineEnd = newline + 1;
    processLine(scanner, line, lineEnd);
    scanner.scanOffset = lineEnd;
  }
  if (scanner.pendingBlankBoundary) {
    const partialLine = text.slice(scanner.scanOffset);
    if (partialLine && !isAmbiguousContinuationPrefix(scanner, partialLine)) {
      setStableBoundary(scanner, scanner.pendingBlankBoundary);
      scanner.blockKind = "";
      scanner.inlineCodeLength = 0;
      scanner.pendingBlankBoundary = 0;
    }
  }
  return { stableBoundary: scanner.stableBoundary, reset };
}
