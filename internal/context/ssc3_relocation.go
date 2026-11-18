package context

import (
	"encoding/binary"
	"time"

	nasie "github.com/free5gc/nas/ie"
	"github.com/free5gc/nas/message"
)

// lab patch 6b/6c: SSC mode 3 PSA relocation with multiple PDU sessions
// (TS 23.502 V19.9.0 §4.3.5.2).
//
// The SMF sends PDU Session Modification Command #39 with the PDU session
// address lifetime, keeps the old session up, and on Modification Complete
// starts T3593 (TS 24.501 V19.8.0 §6.3.2.3). The UE establishes a new PDU
// session that names the old one in Old PDU session ID; the AMF sends it to this
// SMF with oldPduSessionId. When T3593 expires the SMF releases the old session,
// unless the UE has released it already.

// DefaultPDUSessionAddressLifetime is the lifetime sent when the caller gives
// none; it equals the default of T3593 (TS 24.501 table 10.3.2).
const DefaultPDUSessionAddressLifetime = 60 * time.Second

// SSC3Relocation is the state of an SSC mode 3 relocation on the OLD session.
type SSC3Relocation struct {
	OldPSA          string        // UPF name the session was anchored on
	Lifetime        time.Duration // PDU session address lifetime sent to the UE
	CommandSentAt   time.Time
	CompletedAt     time.Time // Modification Complete received; T3593 runs from here
	NewPduSessionID int32     // the PDU session that replaces this one, once known
	T3593           *Timer
}

// Phase is "commanded" (waiting for Modification Complete), "t3593" (timer
// running) or "" for no relocation.
func (r *SSC3Relocation) Phase() string {
	switch {
	case r == nil:
		return ""
	case r.CompletedAt.IsZero():
		return "commanded"
	default:
		return "t3593"
	}
}

// StopT3593 stops the timer, if running.
func (r *SSC3Relocation) StopT3593() {
	if r != nil && r.T3593 != nil {
		r.T3593.Stop()
		r.T3593 = nil
	}
}

// pduSessionAddressLifetimeEPCO is the Extended protocol configuration options
// IE (TS 24.501 §9.11.4.6, TLV-E, IEI 0x7B) with the single network-to-MS
// container 001EH "PDU session address lifetime" (TS 24.008 V19.5.0
// §10.5.6.3): two octets, seconds, big-endian. Octet 3 is 0x80: extension bit
// set, configuration protocol PPP for use with IP PDP type or IP PDN type.
// nas v1.3.0 has no field for this container, so the IE is built here.
func pduSessionAddressLifetimeEPCO(lifetime time.Duration) []byte {
	secs := lifetime / time.Second
	if secs > 0xFFFF {
		secs = 0xFFFF
	}
	if secs < 1 {
		secs = 1
	}
	contents := []byte{0x80, 0x00, 0x1E, 0x02, 0, 0}
	binary.BigEndian.PutUint16(contents[4:], uint16(secs))
	ie := []byte{0x7B, 0, 0}
	binary.BigEndian.PutUint16(ie[1:], uint16(len(contents)))
	return append(ie, contents...)
}

// BuildGSMSSC3RelocationModificationCommand builds the network-requested PDU
// Session Modification Command that asks the UE to relocate an SSC mode 3
// session: no PTI (TS 24.501 §6.3.2.2), 5GSM cause #39 "reactivation requested"
// and the PDU session address lifetime in the ePCO.
func BuildGSMSSC3RelocationModificationCommand(smContext *SMContext, lifetime time.Duration) ([]byte, error) {
	command := &message.PDUSessModCmd{
		PDUSessId: uint8(smContext.PDUSessionID),
		PTI:       0, // "No procedure transaction identity assigned"
		Cause5GSM: &nasie.Cause5GSM{Value: nasie.Cause5GSM_ReactivationReq},
	}
	b, err := command.MarshalBinary()
	if err != nil {
		return nil, err
	}
	// Cause is the only optional IE set before ePCO, so appending keeps the IE
	// order of TS 24.501 §8.3.9.
	return append(b, pduSessionAddressLifetimeEPCO(lifetime)...), nil
}
