package workgroup

import (
	"context"
	"testing"
)

func TestSessionAckPerWorkgroup(t *testing.T) {
	s := &Session{}
	wgA := "wg_01h0000000000000000000000a"
	wgB := "wg_01h0000000000000000000000b"

	if err := s.AckDelivery(wgA, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.AckDelivery(wgB, 1); err != nil {
		t.Fatal(err)
	}
	if got := s.OfferResumeFor(wgA).LastAckDeliverySeq; got != 3 {
		t.Fatalf("wgA=%d", got)
	}
	if got := s.OfferResumeFor(wgB).LastAckDeliverySeq; got != 1 {
		t.Fatalf("wgB=%d", got)
	}
	if got := s.OfferResume().LastAckDeliverySeq; got != 3 {
		t.Fatalf("session max=%d", got)
	}
	if err := s.AckDelivery(wgA, 2); err == nil {
		t.Fatal("expected regress error")
	}
	if err := s.AckDelivery(wgA, 3); err != nil {
		t.Fatal(err)
	}
}

func TestDialerResolveResumeWorkgroups(t *testing.T) {
	d := &Dialer{
		WorkgroupID:  "wg_a",
		WorkgroupIDs: []string{"wg_b", "wg_a", " wg_c "},
		ListWorkgroups: func(ctx context.Context) ([]string, error) {
			return []string{"wg_d", "wg_b"}, nil
		},
	}
	got := d.resolveResumeWorkgroups(context.Background())
	want := []string{"wg_a", "wg_b", "wg_c", "wg_d"}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}
