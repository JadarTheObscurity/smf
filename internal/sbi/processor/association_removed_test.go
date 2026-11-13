package processor

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"

	smf_context "github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/pfcp/pfcptype"
)

func newRemovalTestUPF(t *testing.T, ip string) *smf_context.UPF {
	t.Helper()
	nodeID := pfcptype.NodeID{NodeIdType: pfcptype.NodeIdTypeIpv4Address, IP: net.ParseIP(ip).To4()}
	upf := smf_context.NewUPF(&nodeID, nil)
	t.Cleanup(func() { smf_context.RemoveUPFNodeByNodeID(nodeID) })
	return upf
}

func waitLoopDone(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("association loop did not stop after %s", what)
	}
}

// UPI DELETE of an established UPF (MarkRemoved + CancelAssociation, as
// DeleteUpNodeLink does) must end the lifecycle instead of re-associating.
func TestAssociationLifecycleStopsWhenEstablishedUPFRemoved(t *testing.T) {
	upf := newRemovalTestUPF(t, "192.0.2.71")
	parentContext, cancelParent := context.WithCancel(context.Background())
	t.Cleanup(cancelParent)

	smfSelf := smf_context.GetSelf()
	previousInterval := smfSelf.PfcpHeartbeatInterval
	smfSelf.PfcpHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { smfSelf.PfcpHeartbeatInterval = previousInterval })

	recoveryTime := time.Now().Add(-time.Minute).Truncate(time.Second)
	var setups atomic.Int32
	heartbeats := make(chan struct{}, 100)
	p := &Processor{}
	p.SetActivePFCPClient(&fakeActivePFCPClient{
		setup: func(*net.UDPAddr) (*message.AssociationSetupResponse, error) {
			setups.Add(1)
			return message.NewAssociationSetupResponse(1,
				ie.NewNodeIDHeuristic("192.0.2.71"),
				ie.NewCause(ie.CauseRequestAccepted),
				ie.NewRecoveryTimeStamp(recoveryTime)), nil
		},
		heartbeat: func(*net.UDPAddr) (*message.HeartbeatResponse, error) {
			select {
			case heartbeats <- struct{}{}:
			default:
			}
			return message.NewHeartbeatResponse(2, ie.NewRecoveryTimeStamp(recoveryTime)), nil
		},
	})

	loopDone := make(chan struct{})
	go func() {
		p.ToBeAssociatedWithUPF(parentContext, upf)
		close(loopDone)
	}()
	select {
	case <-heartbeats:
	case <-time.After(time.Second):
		t.Fatal("association loop did not reach heartbeat")
	}

	upf.MarkRemoved()
	upf.CancelAssociation()
	waitLoopDone(t, loopDone, "UPF removal")

	if got := setups.Load(); got != 1 {
		t.Fatalf("Association Setup requests = %d, want 1 (no re-association after removal)", got)
	}
	if got := upf.AssociationState(); got != smf_context.AssociationDown {
		t.Fatalf("AssociationState() = %s, want down", got)
	}
	// Drain, then make sure heartbeats stopped.
	for len(heartbeats) > 0 {
		<-heartbeats
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(heartbeats); n != 0 {
		t.Fatalf("%d heartbeats sent after removal", n)
	}
}

// Removal while Association Setup keeps failing must stop the retry loop
// immediately (the retry timer is an hour here).
func TestAssociationSetupRetryStopsWhenUPFRemoved(t *testing.T) {
	upf := newRemovalTestUPF(t, "192.0.2.72")
	parentContext, cancelParent := context.WithCancel(context.Background())
	t.Cleanup(cancelParent)

	smfSelf := smf_context.GetSelf()
	previousRetryInterval := smfSelf.AssocFailRetryInterval
	smfSelf.AssocFailRetryInterval = time.Hour
	t.Cleanup(func() { smfSelf.AssocFailRetryInterval = previousRetryInterval })

	setupFailed := make(chan struct{}, 10)
	p := &Processor{}
	p.SetActivePFCPClient(&fakeActivePFCPClient{
		setup: func(*net.UDPAddr) (*message.AssociationSetupResponse, error) {
			setupFailed <- struct{}{}
			return nil, errors.New("UPF unreachable")
		},
	})

	loopDone := make(chan struct{})
	go func() {
		p.ToBeAssociatedWithUPF(parentContext, upf)
		close(loopDone)
	}()
	select {
	case <-setupFailed:
	case <-time.After(time.Second):
		t.Fatal("association loop did not try setup")
	}

	upf.MarkRemoved()
	upf.CancelAssociation()
	waitLoopDone(t, loopDone, "UPF removal during setup retry")
	if got := upf.AssociationState(); got != smf_context.AssociationDown {
		t.Fatalf("AssociationState() = %s, want down", got)
	}
}

// Removal must also abort an Association Setup request that is in flight.
func TestAssociationSetupInFlightCanceledWhenUPFRemoved(t *testing.T) {
	upf := newRemovalTestUPF(t, "192.0.2.73")
	parentContext, cancelParent := context.WithCancel(context.Background())
	t.Cleanup(cancelParent)

	started := make(chan struct{}, 1)
	p := &Processor{}
	p.SetActivePFCPClient(&fakeActivePFCPClient{
		setupCtx: func(ctx context.Context, _ *net.UDPAddr) (*message.AssociationSetupResponse, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	loopDone := make(chan struct{})
	go func() {
		p.ToBeAssociatedWithUPF(parentContext, upf)
		close(loopDone)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("association loop did not start setup")
	}
	upf.MarkRemoved()
	waitLoopDone(t, loopDone, "UPF removal with setup in flight")
}

func TestAssociationLifecycleRefusedForRemovedUPF(t *testing.T) {
	upf := newRemovalTestUPF(t, "192.0.2.74")
	upf.MarkRemoved()
	p := &Processor{}
	p.SetActivePFCPClient(&fakeActivePFCPClient{
		setup: func(*net.UDPAddr) (*message.AssociationSetupResponse, error) {
			t.Error("setup must not be attempted for a removed UPF")
			return nil, errors.New("unexpected")
		},
	})
	loopDone := make(chan struct{})
	go func() {
		p.ToBeAssociatedWithUPF(context.Background(), upf)
		close(loopDone)
	}()
	waitLoopDone(t, loopDone, "start on a removed UPF")
}
