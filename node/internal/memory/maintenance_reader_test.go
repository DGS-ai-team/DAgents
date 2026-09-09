package memory

import (
	"context"
	"math"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type testMaintenanceSource struct {
	messages []llm.Message
	revision uint64
	complete bool
}

func (s testMaintenanceSource) LoadMaintenanceMessages(context.Context, string, uint64, int) (DurableMessageBatch, error) {
	return DurableMessageBatch{SessionID: "session-1", Messages: s.messages, Sequence: s.revision, Complete: s.complete}, nil
}

func TestReadDurableMaintenanceInputUsesSQLiteSnapshotAndCursor(t *testing.T) {
	source := testMaintenanceSource{messages: []llm.Message{{Role: "user", Content: "stable preference"}}, revision: 3, complete: true}
	input, sequence, changed, err := ReadDurableMaintenanceInput(context.Background(), source, "agent-1", MaintenanceCursor{})
	if err != nil || !changed || sequence != 3 || len(input.Messages) != 1 {
		t.Fatalf("input=%+v seq=%d changed=%v err=%v", input, sequence, changed, err)
	}
	_, sequence, changed, err = ReadDurableMaintenanceInput(context.Background(), source, "agent-1", MaintenanceCursor{Sequence: sequence, SourceFingerprint: input.SourceFingerprint})
	if err != nil || changed || sequence != 3 {
		t.Fatalf("unchanged seq=%d changed=%v err=%v", sequence, changed, err)
	}
}

func TestReadDurableMaintenanceInputRejectsInvalidSequenceBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		source   testMaintenanceSource
		cursor   MaintenanceCursor
		wantErr  bool
		wantNext int64
	}{
		{name: "negative cursor", source: testMaintenanceSource{complete: true}, cursor: MaintenanceCursor{Sequence: -1}, wantErr: true, wantNext: -1},
		{name: "overflow", source: testMaintenanceSource{revision: math.MaxUint64, complete: true}, cursor: MaintenanceCursor{Sequence: 4}, wantErr: true, wantNext: 4},
		{name: "regression", source: testMaintenanceSource{revision: 2, complete: true}, cursor: MaintenanceCursor{Sequence: 4}, wantErr: true, wantNext: 4},
		{name: "incomplete revision zero", source: testMaintenanceSource{revision: 0, complete: false}, cursor: MaintenanceCursor{Sequence: 4}, wantErr: false, wantNext: 4},
		{name: "incomplete newer unreadable", source: testMaintenanceSource{revision: 5, complete: false}, cursor: MaintenanceCursor{Sequence: 4}, wantErr: true, wantNext: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, next, changed, err := ReadDurableMaintenanceInput(context.Background(), tc.source, "agent-1", tc.cursor)
			if (err != nil) != tc.wantErr || next != tc.wantNext || (tc.wantErr && changed) {
				t.Fatalf("next=%d changed=%v err=%v", next, changed, err)
			}
		})
	}
}
