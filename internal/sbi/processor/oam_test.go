package processor

import (
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/pfcp/pfcptype"
)

func TestPsaUpfInfo(t *testing.T) {
	if got := psaUpfInfo(nil); got != nil {
		t.Fatalf("nil UPNode: got %+v, want nil", got)
	}

	ipv4 := &context.UPNode{
		Name:   "UPF2",
		NodeID: pfcptype.NodeID{NodeIdType: pfcptype.NodeIdTypeIpv4Address, IP: net.ParseIP("10.100.50.242").To4()},
	}
	if got := psaUpfInfo(ipv4); got == nil || got.Name != "UPF2" || got.NodeID != "10.100.50.242" {
		t.Fatalf("IPv4 node: got %+v", got)
	}

	fqdn := &context.UPNode{
		Name:   "UPF",
		NodeID: pfcptype.NodeID{NodeIdType: pfcptype.NodeIdTypeFqdn, FQDN: "upf.free5gc.org"},
	}
	if got := psaUpfInfo(fqdn); got == nil || got.NodeID != "upf.free5gc.org" {
		t.Fatalf("FQDN node: got %+v", got)
	}

	// JSON: field present with a UPF, omitted without one.
	with, err := json.Marshal(PDUSessionInfo{PsaUpf: psaUpfInfo(ipv4)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"PsaUpf":{"Name":"UPF2","NodeID":"10.100.50.242"}`) {
		t.Fatalf("unexpected JSON: %s", with)
	}
	without, err := json.Marshal(PDUSessionInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), "PsaUpf") {
		t.Fatalf("PsaUpf should be omitted: %s", without)
	}
}
