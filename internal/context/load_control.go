package context

import (
	"fmt"
	"net"
	"time"
)

// Where a Load Control Information came from.
const (
	LoadControlSourcePFCP = "pfcp" // LCI IE piggybacked on a PFCP session message (TS 29.244 §6.2.3)
	LoadControlSourceUPI  = "upi"  // injected through the UPI (lab: simulated LCI)
)

// LoadControl is the latest Load Control Information (TS 29.244 §6.2.3) of one UPF.
// Metric is the load of the whole UP function, 0..100 percent.
type LoadControl struct {
	SequenceNumber uint32    `json:"sequenceNumber"`
	Metric         uint8     `json:"metric"`
	ReceivedAt     time.Time `json:"receivedAt"`
	Source         string    `json:"source"`
}

// UpdateLoadControl stores an LCI only if its sequence number is higher than the
// stored one; an equal or lower one is an outdated report and is ignored.
func (upf *UPF) UpdateLoadControl(seq uint32, metric uint8, source string) (bool, error) {
	if metric > 100 {
		return false, fmt.Errorf("load metric %d out of range 0..100", metric)
	}
	upf.lciMu.Lock()
	defer upf.lciMu.Unlock()
	if upf.lci != nil && seq <= upf.lci.SequenceNumber {
		return false, nil
	}
	upf.lci = &LoadControl{SequenceNumber: seq, Metric: metric, ReceivedAt: time.Now(), Source: source}
	return true, nil
}

// LoadControl returns a copy of the stored LCI; ok is false when none was received.
func (upf *UPF) LoadControl() (lc LoadControl, ok bool) {
	upf.lciMu.RLock()
	defer upf.lciMu.RUnlock()
	if upf.lci == nil {
		return LoadControl{}, false
	}
	return *upf.lci, true
}

// ClearLoadControl drops the stored LCI. Called when a PFCP association is
// (re)established: the peer may have restarted and reset its sequence numbers.
func (upf *UPF) ClearLoadControl() {
	upf.lciMu.Lock()
	upf.lci = nil
	upf.lciMu.Unlock()
}

// UpdateUPFLoadControlByIP applies an LCI received from the PFCP peer at ip.
func UpdateUPFLoadControlByIP(ip net.IP, seq uint32, metric uint8, source string) (*UPF, bool, error) {
	var target *UPF
	upfPool.Range(func(_, value interface{}) bool {
		upf := value.(*UPF)
		if upf.NodeID.ResolveNodeIdToIp().Equal(ip) {
			target = upf
			return false
		}
		return true
	})
	if target == nil {
		return nil, false, fmt.Errorf("no UPF with PFCP address %s", ip)
	}
	accepted, err := target.UpdateLoadControl(seq, metric, source)
	return target, accepted, err
}
