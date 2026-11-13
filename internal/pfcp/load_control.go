package pfcp

import (
	"net"

	"github.com/wmnsk/go-pfcp/ie"
)

// cpFeatureLOAD is the LOAD bit of CP Function Features (octet 5 bit 1): the CP
// function supports Load Control, so the UP function may send LCI (TS 29.244 §6.2.3).
const cpFeatureLOAD uint8 = 0x01

// LoadControlHandler consumes the Load Control Information that a UPF
// piggybacks on PFCP session messages (TS 29.244 §6.2.3).
type LoadControlHandler interface {
	HandleLoadControlInformation(peer net.IP, sequenceNumber uint32, metric uint8)
}

func (s *PfcpServer) SetLoadControlHandler(handler LoadControlHandler) {
	s.loadControlMu.Lock()
	s.loadControlHandler = handler
	s.loadControlMu.Unlock()
}

// reportLoadControl decodes an optional LCI IE and hands it to the handler.
// A malformed LCI is logged and ignored; it never fails the carrying message.
func (s *PfcpServer) reportLoadControl(peer net.IP, lci *ie.IE) {
	if lci == nil || peer == nil {
		return
	}
	seq, err := lci.SequenceNumber()
	if err != nil {
		s.log.Warnf("ignore Load Control Information from %v: Sequence Number: %v", peer, err)
		return
	}
	metric, err := lci.Metric()
	if err != nil {
		s.log.Warnf("ignore Load Control Information from %v: Metric: %v", peer, err)
		return
	}
	s.loadControlMu.RLock()
	handler := s.loadControlHandler
	s.loadControlMu.RUnlock()
	if handler != nil {
		handler.HandleLoadControlInformation(peer, seq, metric)
	}
}
