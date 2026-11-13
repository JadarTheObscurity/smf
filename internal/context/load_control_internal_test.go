package context

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/free5gc/smf/internal/pfcp/pfcptype"
)

func newTestLCIUPF(t *testing.T, ip string) *UPF {
	t.Helper()
	nodeID := &pfcptype.NodeID{NodeIdType: pfcptype.NodeIdTypeIpv4Address, IP: net.ParseIP(ip).To4()}
	upf := NewUPF(nodeID, nil)
	t.Cleanup(func() { RemoveUPFNodeByNodeID(*nodeID) })
	return upf
}

func TestUpdateLoadControlSequenceAndRange(t *testing.T) {
	upf := newTestLCIUPF(t, "10.250.0.1")
	_, ok := upf.LoadControl()
	require.False(t, ok)

	accepted, err := upf.UpdateLoadControl(5, 85, LoadControlSourceUPI)
	require.NoError(t, err)
	require.True(t, accepted)

	accepted, err = upf.UpdateLoadControl(5, 10, LoadControlSourceUPI)
	require.NoError(t, err)
	require.False(t, accepted, "equal sequence number is outdated")
	accepted, _ = upf.UpdateLoadControl(4, 10, LoadControlSourceUPI)
	require.False(t, accepted, "lower sequence number is outdated")

	_, err = upf.UpdateLoadControl(6, 101, LoadControlSourceUPI)
	require.Error(t, err)

	lc, ok := upf.LoadControl()
	require.True(t, ok)
	require.Equal(t, uint32(5), lc.SequenceNumber)
	require.Equal(t, uint8(85), lc.Metric)

	upf.ClearLoadControl()
	accepted, _ = upf.UpdateLoadControl(1, 20, LoadControlSourcePFCP)
	require.True(t, accepted, "a cleared UPF accepts a restarted sequence")
}

func TestUpdateUPFLoadControlByIP(t *testing.T) {
	upf := newTestLCIUPF(t, "10.250.0.2")
	got, accepted, err := UpdateUPFLoadControlByIP(net.ParseIP("10.250.0.2"), 1, 40, LoadControlSourcePFCP)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Same(t, upf, got)

	_, _, err = UpdateUPFLoadControlByIP(net.ParseIP("10.250.0.99"), 1, 40, LoadControlSourcePFCP)
	require.Error(t, err)
}

func TestSortUPFListByLoadUsesMetricThenSessions(t *testing.T) {
	a := &UPNode{Type: UPNODE_UPF, NodeID: pfcptype.NodeID{IP: net.ParseIP("10.250.1.1").To4()}}
	b := &UPNode{Type: UPNODE_UPF, NodeID: pfcptype.NodeID{IP: net.ParseIP("10.250.1.2").To4()}}
	c := &UPNode{Type: UPNODE_UPF, NodeID: pfcptype.NodeID{IP: net.ParseIP("10.250.1.3").To4()}}
	a.UPF = newTestLCIUPF(t, "10.250.1.1")
	b.UPF = newTestLCIUPF(t, "10.250.1.2")
	c.UPF = newTestLCIUPF(t, "10.250.1.3")
	upi := &UserPlaneInformation{}

	// a: 85, b: 30, c: no LCI (counts as 0)
	_, _ = a.UPF.UpdateLoadControl(1, 85, LoadControlSourceUPI)
	_, _ = b.UPF.UpdateLoadControl(1, 30, LoadControlSourceUPI)
	require.Equal(t, []*UPNode{c, b, a}, upi.sortUPFListByLoad([]*UPNode{a, b, c}))

	// all equal: input (name) order is kept
	_, _ = b.UPF.UpdateLoadControl(2, 85, LoadControlSourceUPI)
	_, _ = c.UPF.UpdateLoadControl(1, 85, LoadControlSourceUPI)
	require.Equal(t, []*UPNode{a, b, c}, upi.sortUPFListByLoad([]*UPNode{a, b, c}))
}
