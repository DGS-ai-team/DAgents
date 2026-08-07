package hostsnapshot

import (
	"net"
	"testing"
)

func TestFormatHostIPs_filtersAndOrders(t *testing.T) {
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("169.254.1.2"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)}, // dup
	}
	got := FormatHostIPs(addrs)
	want := "10.0.0.5;192.168.1.10;2001:db8::1"
	if got != want {
		t.Fatalf("FormatHostIPs = %q, want %q", got, want)
	}
}

func TestFormatHostIPs_empty(t *testing.T) {
	if got := FormatHostIPs(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}
