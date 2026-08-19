import { describe, expect, it } from "vitest";
import {
  formatPathsForComposer,
  formatFileReferences,
  buildMessageWithFileReferences,
  mergePathInsertion,
  pathsFromFileList,
  pathsFromUriList,
  shouldResolvePathsViaShell,
} from "./filePathPaste.js";

describe("shouldResolvePathsViaShell", () => {
  it("skips plain text paste", () => {
    expect(shouldResolvePathsViaShell({ text: "hello", files: [] })).toBe(false);
  });

  it("resolves when files present without text", () => {
    expect(shouldResolvePathsViaShell({ text: "", files: [{}] })).toBe(true);
  });

  it("resolves when no text and no files (CF_HDROP-only clipboard)", () => {
    expect(shouldResolvePathsViaShell({ text: "", files: [] })).toBe(true);
  });
});

describe("pathsFromUriList", () => {
  it("parses file URI on Windows", () => {
    expect(pathsFromUriList("file:///D:/temp/snow.png")).toEqual(["D:\\temp\\snow.png"]);
  });

  it("ignores comments and http", () => {
    expect(pathsFromUriList("# comment\nhttp://x\nfile:///C:/a.txt")).toEqual(["C:\\a.txt"]);
  });
});

describe("pathsFromFileList", () => {
  it("uses legacy file.path when present", () => {
    const files = [{ path: "D:\\x\\y.txt" }];
    expect(pathsFromFileList(files)).toEqual(["D:\\x\\y.txt"]);
  });
});

describe("formatPathsForComposer", () => {
  it("joins multiple paths with newline", () => {
    expect(formatPathsForComposer(["D:\\a", "D:\\b"])).toBe("D:\\a\nD:\\b");
  });
});

describe("file references", () => {
  it("formats each path as a model-readable file reference", () => {
    expect(formatFileReferences(["D:\\a.txt", "D:\\b.txt", "D:\\a.txt"])).toBe(
      "'''引用文件：<file>D:\\a.txt</file>'''\n'''引用文件：<file>D:\\b.txt</file>'''",
    );
  });

  it("prepends references without changing the user text", () => {
    expect(buildMessageWithFileReferences("请读取", ["D:\\a.txt"])).toBe(
      "'''引用文件：<file>D:\\a.txt</file>'''\n\n请读取",
    );
  });
});

describe("mergePathInsertion", () => {
  it("inserts at cursor with separator", () => {
    const { value, cursor } = mergePathInsertion("hello", "D:\\a.txt", { start: 5, end: 5 });
    expect(value).toBe("hello\nD:\\a.txt");
    expect(cursor).toBe(value.length);
  });

  it("replaces selection", () => {
    const { value } = mergePathInsertion("abc def", "D:\\x", { start: 4, end: 7 });
    expect(value).toBe("abc D:\\x");
  });
});
