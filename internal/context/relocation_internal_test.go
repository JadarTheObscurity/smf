package context

import (
	"testing"
	"time"
)

func TestRelocationHint(t *testing.T) {
	now := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	relocationNow = func() time.Time { return now }
	defer func() { relocationNow = time.Now }()

	snssai := &SNssai{Sst: 1, Sd: "010203"}
	SetRelocationHint("imsi-208930000000002", "internet", snssai, "UPF")

	// Other UE, DNN or slice: no hint.
	if _, ok := TakeRelocationHint("imsi-208930000000003", "internet", snssai); ok {
		t.Fatal("hint leaked to another SUPI")
	}
	if _, ok := TakeRelocationHint("imsi-208930000000002", "ims", snssai); ok {
		t.Fatal("hint leaked to another DNN")
	}
	if _, ok := TakeRelocationHint("imsi-208930000000002", "internet", &SNssai{Sst: 1, Sd: "112233"}); ok {
		t.Fatal("hint leaked to another S-NSSAI")
	}

	// Same key, an equal but different *SNssai: found once.
	got, ok := TakeRelocationHint("imsi-208930000000002", "internet", &SNssai{Sst: 1, Sd: "010203"})
	if !ok || got != "UPF" {
		t.Fatalf("got %q %v, want UPF true", got, ok)
	}
	if _, ok = TakeRelocationHint("imsi-208930000000002", "internet", snssai); ok {
		t.Fatal("hint must be used only once")
	}

	// Expired hint.
	SetRelocationHint("imsi-208930000000002", "internet", snssai, "UPF2")
	now = now.Add(RelocationHintTTL + time.Second)
	if _, ok = TakeRelocationHint("imsi-208930000000002", "internet", snssai); ok {
		t.Fatal("expired hint returned")
	}
}

func TestDeprioritizeUPF(t *testing.T) {
	upi := &UserPlaneInformation{}
	a, b, c := &UPNode{Name: "UPF"}, &UPNode{Name: "UPF2"}, &UPNode{Name: "UPF3"}
	names := func(l []*UPNode) (s []string) {
		for _, n := range l {
			s = append(s, n.Name)
		}
		return s
	}
	cases := []struct {
		avoid string
		want  []string
	}{
		{"", []string{"UPF", "UPF2", "UPF3"}},
		{"UPF", []string{"UPF2", "UPF3", "UPF"}},
		{"UPF2", []string{"UPF", "UPF3", "UPF2"}},
		{"nope", []string{"UPF", "UPF2", "UPF3"}},
	}
	for _, tc := range cases {
		got := names(upi.deprioritizeUPF([]*UPNode{a, b, c}, tc.avoid))
		if len(got) != len(tc.want) {
			t.Fatalf("avoid %q: got %v, want %v", tc.avoid, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("avoid %q: got %v, want %v", tc.avoid, got, tc.want)
			}
		}
	}
	// The only UPF stays selectable.
	if got := names(upi.deprioritizeUPF([]*UPNode{a}, "UPF")); len(got) != 1 || got[0] != "UPF" {
		t.Fatalf("single UPF: got %v", got)
	}
}
