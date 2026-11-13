package context_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	smf_context "github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/pfcp/pfcptype"
)

func TestUPFMarkRemoved(t *testing.T) {
	// A zero-value UPF (as some tests build it) must work too.
	upf := &smf_context.UPF{NodeID: pfcptype.NodeID{
		NodeIdType: pfcptype.NodeIdTypeIpv4Address,
		IP:         net.ParseIP("192.0.2.70").To4(),
	}}
	require.False(t, upf.IsRemoved())

	removed := upf.Removed()
	lifecycle, cancel := upf.LifecycleContext(context.Background())
	t.Cleanup(cancel)
	association := upf.EstablishAssociation(lifecycle)
	select {
	case <-removed:
		t.Fatal("Removed() closed before MarkRemoved")
	default:
	}

	upf.MarkRemoved()
	upf.MarkRemoved() // idempotent, must not panic on a second close

	require.True(t, upf.IsRemoved())
	for name, done := range map[string]<-chan struct{}{
		"Removed() taken before": removed,
		"Removed() taken after":  upf.Removed(),
		"lifecycle context":      lifecycle.Done(),
		"association context":    association.Done(),
	} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s was not closed by MarkRemoved", name)
		}
	}

	upf.CancelAssociation()
	require.False(t, upf.BeginAssociationSetup(), "setup must be refused after removal")
	require.False(t, upf.BeginAssociationLifecycle(), "lifecycle must be refused after removal")
}

func TestUPFLifecycleContextFollowsParent(t *testing.T) {
	upf := &smf_context.UPF{}
	parent, cancelParent := context.WithCancel(context.Background())
	lifecycle, cancel := upf.LifecycleContext(parent)
	t.Cleanup(cancel)
	cancelParent()
	select {
	case <-lifecycle.Done():
	case <-time.After(time.Second):
		t.Fatal("lifecycle context did not follow its parent")
	}
	require.False(t, upf.IsRemoved())
}

func TestUpNodeDeleteMarksUPFRemoved(t *testing.T) {
	upi, err := smf_context.NewUserPlaneInformation(configuration)
	require.NoError(t, err)
	upNode := upi.UPFs["UPF4"]
	require.NotNil(t, upNode)
	upf := upNode.UPF
	require.False(t, upf.IsRemoved())

	upi.UpNodeDelete("UPF4")

	require.Nil(t, upi.UPFs["UPF4"])
	require.True(t, upf.IsRemoved())
	require.True(t, upi.UPFs["UPF3"] != nil && !upi.UPFs["UPF3"].UPF.IsRemoved(),
		"other UPFs must not be marked removed")
}
