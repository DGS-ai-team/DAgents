import { describe, expect, it } from "vitest";
import {
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

});
