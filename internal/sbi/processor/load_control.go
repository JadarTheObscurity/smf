package processor

import (
	"net"

	smf_context "github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/logger"
)

// HandleLoadControlInformation stores an LCI that a UPF sent on a PFCP session
// message (TS 29.244 §6.2.3). UPF selection reads it in SelectUPFAndAllocUEIP.
func (p *Processor) HandleLoadControlInformation(peer net.IP, sequenceNumber uint32, metric uint8) {
	_, accepted, err := smf_context.UpdateUPFLoadControlByIP(
		peer, sequenceNumber, metric, smf_context.LoadControlSourcePFCP)
	switch {
	case err != nil:
		logger.PfcpLog.Warnf("ignore Load Control Information from %v: %v", peer, err)
	case !accepted:
		logger.PfcpLog.Debugf("ignore outdated Load Control Information from %v (sequence %d)", peer, sequenceNumber)
	default:
		logger.PfcpLog.Infof("UPF[%v] Load Control Information: sequence %d, metric %d (pfcp)",
			peer, sequenceNumber, metric)
	}
}
