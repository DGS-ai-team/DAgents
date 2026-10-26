import { describe, expect, it } from "vitest";
import {
  buildReadFilePreview,
  fileExtension,
  readFilePreviewMode,
  splitReadFileOutput,
} from "./readFilePreview.js";

describe("splitReadFileOutput", () => {
  it("splits meta and body at ---", () => {
    const out = splitReadFileOutput("文件总行数: 1\n---\nhello");
    expect(out.meta).toContain("文件总行数");
    expect(out.body).toBe("hello");
  });
});

describe("readFilePreviewMode", () => {
  it("detects markdown and html", () => {
    expect(readFilePreviewMode("docs/README.md")).toBe("markdown");
    expect(readFilePreviewMode("page/index.html")).toBe("html");
    expect(readFilePreviewMode("data/config.json")).toBe("json");
    expect(readFilePreviewMode("sheet.csv")).toBe("csv");
    expect(readFilePreviewMode("main.go")).toBe("code");
  });
});

describe("buildReadFilePreview", () => {
  it("renders markdown html", () => {
    const preview = buildReadFilePreview("a.md", "文件总行数: 1\n---\n# Title");
    expect(preview.mode).toBe("markdown");
    expect(preview.html).toContain("<h1>Title</h1>");
  });

  it("formats json body", () => {
    const preview = buildReadFilePreview("x.json", "文件总行数: 1\n---\n{\"a\":1}");
    expect(preview.jsonText).toContain('"a": 1');
  });

  it("parses csv rows", () => {
    const preview = buildReadFilePreview("t.csv", "文件总行数: 2\n---\na,b\n1,2");
    expect(preview.csvRows).toEqual([
      ["a", "b"],
      ["1", "2"],
    ]);
  });
});

describe("fileExtension", () => {
  it("handles windows paths", () => {
    expect(fileExtension("C:\\proj\\file.HTML")).toBe("html");
  });
});
