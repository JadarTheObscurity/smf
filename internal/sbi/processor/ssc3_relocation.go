package processor

import (
	"fmt"
	"time"

	nasie "github.com/free5gc/nas/ie"
	"github.com/free5gc/openapi/models"
	smf_context "github.com/free5gc/smf/internal/context"
)

// lab patch 6b/6c: SSC mode 3 PSA relocation with multiple PDU sessions
// (TS 23.502 V19.9.0 §4.3.5.2, TS 24.501 V19.8.0 §6.3.2).

// startSSC3Relocation sends the PDU Session Modification Command #39 with the
// PDU session address lifetime (step 2) and records the relocation on the old
// session. The session stays Active and keeps carrying traffic. The caller
// holds SMLock.
func (p *Processor) startSSC3Relocation(smContext *smf_context.SMContext, oldPSA string, lifetime time.Duration) error {
	buf, err := smf_context.BuildGSMSSC3RelocationModificationCommand(smContext, lifetime)
	if err != nil {
		return fmt.Errorf("build PDU Session Modification Command: %w", err)
	}

	// N1 only; smfReallocationInd is left false, so the AMF sends the new PDU
	// session to this SMF (TS 23.502 §4.3.5.2 step 4).
	n1n2Request := models.N1N2MessageTransferRequestBody{
		JsonData: &models.Amf_Comm_N1N2MessageTransferReqData{
			PduSessionId: smContext.PDUSessionID,
			N1MessageContainer: &models.Amf_Comm_N1MessageContainer{
				N1MessageClass:   "SM",
				N1MessageContent: &models.RefToBinaryData{ContentId: "GSM_NAS"},
			},
		},
		BinaryDataN1Message: related("GSM_NAS", buf),
	}
	ctx, _, err := smf_context.GetSelf().GetTokenCtx(
		models.Nrf_NFMgmt_ServiceName_NAMF_COMM, models.Nrf_NFMgmt_NFType_AMF)
	if err != nil {
		return fmt.Errorf("get namf-comm token: %w", err)
	}
	rsp, err := p.Consumer().N1N2MessageTransfer(ctx, smContext.Supi, n1n2Request,
		smContext.CommunicationClientApiPrefix)
	if err != nil || rsp == nil {
		return fmt.Errorf("N1N2MessageTransfer of the Modification Command failed: %v", err)
	}
	if rsp.Cause != models.Amf_Comm_N1N2MessageTransferCause_N1_N2_TRANSFER_INITIATED {
		return fmt.Errorf("N1N2MessageTransfer of the Modification Command: %s", rsp.Cause)
	}

	smContext.SSC3Relocation = &smf_context.SSC3Relocation{
		OldPSA:        oldPSA,
		Lifetime:      lifetime,
		CommandSentAt: time.Now(),
	}
	// T3591 retransmits the command until Modification Complete arrives.
	p.sendGSMPDUSessionModificationCommand(smContext, buf)
	smContext.Log.Infof("SSC mode 3 relocation: Modification Command #39 sent, address lifetime %s", lifetime)
	return nil
}

// onSSC3ModificationComplete starts T3593 when the UE completes the Modification
// Command #39 (TS 24.501 §6.3.2.3): with the lifetime sent to the UE. When it
// expires, the old session is released. The caller holds SMLock.
func (p *Processor) onSSC3ModificationComplete(smContext *smf_context.SMContext) {
	r := smContext.SSC3Relocation
	if r == nil || !r.CompletedAt.IsZero() {
		return
	}
	r.CompletedAt = time.Now()
	ref := smContext.Ref
	// NewTimer calls cancelFunc on tick maxRetryTimes+1, so 0 fires once,
	// after Lifetime.
	r.T3593 = smf_context.NewTimer(r.Lifetime, 0, func(int32) {}, func() {
		p.onT3593Expiry(ref, smContext)
	})
	smContext.Log.Infof("SSC mode 3 relocation: Modification Complete, T3593 started (%s)", r.Lifetime)
}

// onT3593Expiry releases the old PDU session with #36 "regular deactivation",
// so the UE does not re-establish it (it already uses the new one). Nothing is
// done if the session is gone or no longer Active (e.g. the UE released it).
func (p *Processor) onT3593Expiry(ref string, smContext *smf_context.SMContext) {
	smContext.SMLock.Lock()
	defer smContext.SMLock.Unlock()
	if smContext.SSC3Relocation != nil {
		smContext.SSC3Relocation.T3593 = nil
	}
	if smf_context.GetSMContextByRef(ref) != smContext || smContext.State() != smf_context.Active {
		smContext.Log.Infof("SSC mode 3 relocation: T3593 expired, old PDU session already %s", smContext.State())
		return
	}
	newPsi := int32(0)
	if smContext.SSC3Relocation != nil {
		newPsi = smContext.SSC3Relocation.NewPduSessionID
	}
	smContext.Log.Infof("SSC mode 3 relocation: T3593 expired, releasing old PDU session (new PDU session %d)", newPsi)
	if err := p.releasePDUSessionByNetwork(smContext, nasie.Cause5GSM_RegularDeactivation); err != nil {
		smContext.Log.Warnf("SSC mode 3 relocation: release of the old PDU session: %v", err)
	}
}

// linkSSC3Relocation handles the Old PDU Session ID of a new PDU session
// (TS 23.502 §4.3.5.2 step 4): the SMF stores the new PDU session ID on the old
// session. The new PSA is chosen by UPF selection, which tries the old PSA last
// through the relocation hint set when the command was sent.
func linkSSC3Relocation(newCtx *smf_context.SMContext, oldPduSessionID int32) {
	if oldPduSessionID == 0 {
		return
	}
	newCtx.ReplacesPDUSessionID = oldPduSessionID
	old := smf_context.GetSMContextById(newCtx.Supi, oldPduSessionID)
	if old == nil {
		newCtx.Log.Warnf("SSC mode 3 relocation: old PDU session %d not found", oldPduSessionID)
		return
	}
	old.SMLock.Lock()
	defer old.SMLock.Unlock()
	if old.SSC3Relocation == nil {
		newCtx.Log.Warnf("SSC mode 3 relocation: old PDU session %d is not being relocated", oldPduSessionID)
		return
	}
	old.SSC3Relocation.NewPduSessionID = newCtx.PDUSessionID
	newCtx.Log.Infof("SSC mode 3 relocation: PDU session %d replaces PDU session %d (old PSA %s)",
		newCtx.PDUSessionID, oldPduSessionID, old.SSC3Relocation.OldPSA)
}
