import { describe, expect, it } from "vitest";
import {
  extractFileReferencesFromMessage,
  fileReferenceKey,
  normalizeFileReferences,
  normalizeFilePath,
  pathsFromUriList,
} from "./filePathPaste.js";

describe("file references", () => {
  it("keeps Unix URI paths as Unix paths", () => {
    expect(pathsFromUriList("file:///tmp/notes.txt")).toEqual(["/tmp/notes.txt"]);
  });

  it("normalizes Windows separators and deduplicates case-insensitively", () => {
    const refs = normalizeFileReferences([
      { path: "D:/work/Readme.md" },
      { path: "d:\\work\\README.md" },
    ]);
    expect(refs).toHaveLength(1);
    expect(refs[0].name).toBe("Readme.md");
    expect(normalizeFilePath("D:/work/Readme.md")).toBe("D:\\work\\Readme.md");
    expect(fileReferenceKey(refs[0].path)).toBe("d:\\work\\readme.md");
  });

  it("removes legacy markers from displayed text", () => {
    expect(extractFileReferencesFromMessage("'''引用文件：<file>D:\\a.txt</file>'''\n\n读取它")).toEqual({
      text: "读取它",
      fileRefs: [expect.objectContaining({ path: "D:\\a.txt", name: "a.txt" })],
    });
  });
});
