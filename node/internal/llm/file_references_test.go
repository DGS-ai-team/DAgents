package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildUserMessageWithFileReferencesKeepsVisibleTextClean(t *testing.T) {
	msg, err := BuildUserMessageWithFileReferences("请读取", nil, UserNameHuman, []FileReference{{Path: `D:/work/README.md`}})
	if err != nil {
		t.Fatalf("BuildUserMessageWithFileReferences: %v", err)
	}
	if msg.Content != "请读取" || len(msg.FileReferences) != 1 {
		t.Fatalf("message = %+v", msg)
	}
	payload, err := MessageToAPIPayload(msg)
	if err != nil {
		t.Fatalf("MessageToAPIPayload: %v", err)
	}
	content, err := json.Marshal(payload["content"])
	if err != nil || !strings.Contains(string(content), "README.md") || !strings.Contains(string(content), "请读取") {
		t.Fatalf("model payload content = %#v", payload["content"])
	}
	if _, ok := payload["file_refs"]; ok {
		t.Fatalf("internal file_refs leaked into provider payload: %#v", payload)
	}
}

func TestFileReferencesAreIncludedInJournalButNotVisibleSummary(t *testing.T) {
	msg, err := BuildUserMessageWithFileReferences("", nil, UserNameHuman, []FileReference{{Path: "/tmp/a.txt"}})
	if err != nil {
		t.Fatalf("BuildUserMessageWithFileReferences: %v", err)
	}
	if MessageTextSummary(msg) != "" {
		t.Fatalf("summary = %q", MessageTextSummary(msg))
	}
	journal := MessageToJournalPayload(msg)
	if _, ok := journal["file_refs"]; !ok {
		t.Fatalf("journal omitted file_refs: %#v", journal)
	}
}

func TestNormalizeFileReferencesDeduplicatesWindowsPaths(t *testing.T) {
	refs, err := NormalizeFileReferences([]FileReference{
		{Path: `D:/work/README.md`},
		{Path: `d:\work\README.md`},
	})
	if err != nil {
		t.Fatalf("NormalizeFileReferences: %v", err)
	}
	if len(refs) != 1 || refs[0].Path != `D:\work\README.md` || refs[0].Name != "README.md" {
		t.Fatalf("refs = %+v", refs)
	}
}
