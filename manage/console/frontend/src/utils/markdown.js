/** 简易 Markdown（工作组对话 assistant 消息） */
export function renderMarkdown(text) {
  const normalized = String(text || "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const blocks = splitMarkdownBlocks(normalized);
  return blocks.map(renderMarkdownBlock).join("\n");
}

function escapeHtml(text) {
  return String(text || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function renderInline(text) {
  return escapeHtml(text)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/\*([^*]+)\*/g, "<em>$1</em>")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');
}

function splitMarkdownBlocks(text) {
  const lines = text.split("\n");
  const blocks = [];
  let i = 0;

  while (i < lines.length) {
    while (i < lines.length && lines[i].trim() === "") i += 1;
    if (i >= lines.length) break;

    if (/^```/.test(lines[i])) {
      const start = i + 1;
      i += 1;
      while (i < lines.length && !/^```/.test(lines[i])) i += 1;
      const end = i < lines.length ? i : lines.length;
      blocks.push({ type: "code", content: lines.slice(start, end).join("\n") });
      if (i < lines.length) i += 1;
      continue;
    }

    if (isHorizontalRule(lines[i])) {
      blocks.push({ type: "hr" });
      i += 1;
      continue;
    }

    const heading = lines[i].match(/^(#{1,3}) (.+)$/);
    if (heading) {
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2] });
      i += 1;
      continue;
    }

    if (isTableStart(lines, i)) {
      const start = i;
      i += 2;
      while (i < lines.length && isTableRow(lines[i])) i += 1;
      blocks.push({ type: "table", lines: lines.slice(start, i) });
      continue;
    }

    if (isUnorderedListItem(lines[i])) {
      const items = [];
      while (i < lines.length && isUnorderedListItem(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*+]\s+/, ""));
        i += 1;
      }
      blocks.push({ type: "ul", items });
      continue;
    }

    if (isOrderedListItem(lines[i])) {
      const items = [];
      while (i < lines.length && isOrderedListItem(lines[i])) {
        items.push(lines[i].replace(/^\s*\d+[.)]\s+/, ""));
        i += 1;
      }
      blocks.push({ type: "ol", items });
      continue;
    }

    const start = i;
    while (i < lines.length && lines[i].trim() !== "") {
      if (/^```/.test(lines[i])) break;
      if (isHorizontalRule(lines[i])) break;
      if (/^#{1,3} /.test(lines[i])) break;
      if (isTableStart(lines, i)) break;
      if (isUnorderedListItem(lines[i])) break;
      if (isOrderedListItem(lines[i])) break;
      i += 1;
    }
    // 防御：分类条件与段落扫描不一致时避免空段落死循环（例如历史 CRLF 边角）
    if (i === start) {
      blocks.push({ type: "paragraph", content: lines[i] });
      i += 1;
      continue;
    }
    blocks.push({ type: "paragraph", content: lines.slice(start, i).join("\n") });
  }

  return blocks;
}

function renderMarkdownBlock(block) {
  switch (block.type) {
    case "code":
      return `<pre><code>${escapeHtml(block.content)}</code></pre>`;
    case "hr":
      return "<hr>";
    case "heading":
      return `<h${block.level}>${renderInline(block.text)}</h${block.level}>`;
    case "table":
      return renderMarkdownTable(block.lines);
    case "ul":
      return `<ul>${block.items.map((item) => `<li>${renderInline(item)}</li>`).join("")}</ul>`;
    case "ol":
      return `<ol>${block.items.map((item) => `<li>${renderInline(item)}</li>`).join("")}</ol>`;
    case "paragraph":
      return renderMarkdownParagraph(block.content);
    default:
      return "";
  }
}

function renderMarkdownParagraph(content) {
  const parts = String(content || "")
    .split("\n")
    .map((line) => renderInline(line));
  if (parts.length <= 1) {
    return `<p>${parts[0] || ""}</p>`;
  }
  return `<p>${parts.join("<br>")}</p>`;
}

function renderMarkdownTable(lines) {
  if (!Array.isArray(lines) || lines.length < 2) return "";
  const headers = parseTableRow(lines[0]);
  const rows = lines.slice(2).map(parseTableRow);
  let html = "<table><thead><tr>";
  for (const cell of headers) {
    html += `<th>${renderInline(cell)}</th>`;
  }
  html += "</tr></thead><tbody>";
  for (const row of rows) {
    html += "<tr>";
    for (let c = 0; c < headers.length; c += 1) {
      html += `<td>${renderInline(row[c] || "")}</td>`;
    }
    html += "</tr>";
  }
  html += "</tbody></table>";
  return html;
}

function parseTableRow(line) {
  let row = String(line || "").trim();
  if (row.startsWith("|")) row = row.slice(1);
  if (row.endsWith("|")) row = row.slice(0, -1);
  return row.split("|").map((cell) => cell.trim());
}

function isTableRow(line) {
  return String(line || "").includes("|");
}

function isTableSeparator(line) {
  const trimmed = String(line || "").trim();
  if (!trimmed.includes("-")) return false;
  const cells = parseTableRow(trimmed);
  return cells.length > 0 && cells.every((cell) => /^:?-+:?$/.test(cell));
}

function isTableStart(lines, index) {
  if (index + 1 >= lines.length) return false;
  if (!isTableRow(lines[index])) return false;
  return isTableSeparator(lines[index + 1]);
}

function isHorizontalRule(line) {
  const trimmed = String(line || "").trim();
  if (trimmed.includes("|")) return false;
  return /^(?:-{3,}|\*{3,}|_{3,})$/.test(trimmed);
}

function isUnorderedListItem(line) {
  return /^\s*[-*+]\s+\S/.test(String(line || ""));
}

function isOrderedListItem(line) {
  return /^\s*\d+[.)]\s+\S/.test(String(line || ""));
}
