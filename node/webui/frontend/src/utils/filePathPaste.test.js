import { describe, expect, it } from "vitest";
import {
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
