package sbi

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	smf_context "github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/pfcp/pfcptype"
)

func TestLoadControlEntryAssociation(t *testing.T) {
	nodeID := pfcptype.NodeID{NodeIdType: pfcptype.NodeIdTypeIpv4Address, IP: net.ParseIP("10.250.4.1").To4()}
	upf := smf_context.NewUPF(&nodeID, nil)
	t.Cleanup(func() { smf_context.RemoveUPFNodeByNodeID(nodeID) })
	node := &smf_context.UPNode{Name: "UPFX", Type: smf_context.UPNODE_UPF, NodeID: nodeID, UPF: upf}

	entry := loadControlEntry(node)
	require.Equal(t, "10.250.4.1", entry["nodeID"])
	require.Equal(t, 0, entry["sessions"])
	require.Equal(t, "down", entry["association"])
	require.Equal(t, uint64(0), entry["associationGeneration"])
	require.NotContains(t, entry, "lci")

	require.True(t, upf.BeginAssociationSetup())
	require.Equal(t, "setting-up", loadControlEntry(node)["association"])

	upf.EstablishAssociation(context.Background())
	entry = loadControlEntry(node)
	require.Equal(t, "established", entry["association"])
	require.Equal(t, uint64(1), entry["associationGeneration"])

	_, err := upf.UpdateLoadControl(1, 40, smf_context.LoadControlSourceUPI)
	require.NoError(t, err)
	require.Contains(t, loadControlEntry(node), "lci")

	require.True(t, upf.BeginAssociationRelease())
	require.Equal(t, "releasing", loadControlEntry(node)["association"])

	// Re-establishing bumps the generation and clears the stored LCI.
	upf.EstablishAssociation(context.Background())
	entry = loadControlEntry(node)
	require.Equal(t, "established", entry["association"])
	require.Equal(t, uint64(2), entry["associationGeneration"])
	require.NotContains(t, entry, "lci")

	// Tearing the association down (heartbeat loss, Recovery Time Stamp change)
	// also bumps the generation: a UPF restart shows up as +2 once re-established.
	upf.CancelAssociation()
	entry = loadControlEntry(node)
	require.Equal(t, "down", entry["association"])
	require.Equal(t, uint64(3), entry["associationGeneration"])
	require.True(t, upf.BeginAssociationSetup())
	upf.EstablishAssociation(context.Background())
	require.Equal(t, uint64(4), loadControlEntry(node)["associationGeneration"])
}
