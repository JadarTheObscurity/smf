package processor

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	nasie "github.com/free5gc/nas/ie"
	smf_context "github.com/free5gc/smf/internal/context"
)

// lab patch 5: network-triggered PSA relocation of one PDU session.
//
// POST /upi/v1/sessions/{smContextRef}/relocate picks the procedure from the
// SSC mode the session was established with:
//   - SSC mode 2: TS 23.502 V19.9.0 §4.3.5.1. The SMF releases the session with
//     PDU Session Release Command #39 "reactivation requested" (TS 24.501
//     V19.8.0 §6.3.3.2); the UE re-establishes it with a new PDU session ID and
//     the SMF selects the new PSA, trying the old one last (relocation hint).
//   - SSC mode 1: the PSA is kept for the life of the session (TS 23.501 V19.9.0
//     §5.6.9.2.1), so this is refused unless force is set. TS 23.501 §5.6.9.3
//     NOTE 2 still allows a release with reactivation for restoration or user
//     plane path optimization, at the cost of an interruption.
//   - SSC mode 3: make-before-break, TS 23.502 §4.3.5.2 (lab patch 6b/6c, see
//     ssc3_relocation.go): PDU Session Modification Command #39 with the PDU
//     session address lifetime; the old session stays up until T3593 expires
//     or the UE releases it.

// relocationTimeout bounds the PFCP Session Deletion of the old session.
const relocationTimeout = 10 * time.Second

// Relocation procedures, as reported by the API.
const (
	relocationRelease39      = "PDU Session Release Command #39"
	relocationModification39 = "PDU Session Modification Command #39"
)

// relocationDecision checks whether a session in this state and SSC mode can
// be relocated. It returns the procedure to run, or the HTTP status and reason
// for refusing. A selected mode of 0 (session created before patch 6a) counts
// as SSC mode 1.
func relocationDecision(state smf_context.SMContextState, sscMode uint8, force bool) (
	procedure string, status int, reason string,
) {
	if state != smf_context.Active {
		return "", http.StatusConflict, fmt.Sprintf("PDU session state is %s, not Active", state)
	}
	switch sscMode {
	case nasie.SSCMODE2:
		return relocationRelease39, http.StatusAccepted, ""
	case nasie.SSCMODE3:
		return relocationModification39, http.StatusAccepted, ""
	default:
		if force {
			return relocationRelease39, http.StatusAccepted, ""
		}
		return "", http.StatusConflict,
			"SSC mode 1 keeps its PSA for the life of the session (TS 23.501 §5.6.9.2.1); " +
				"set force=true to release it with #39 anyway (TS 23.501 §5.6.9.3 NOTE 2)"
	}
}

// RelocatePDUSession runs the PSA relocation of the PDU session with this SM
// context reference and returns the HTTP status and body for the UPI. lifetime
// is the PDU session address lifetime for SSC mode 3 (0: the default).
func (p *Processor) RelocatePDUSession(smContextRef string, force bool, lifetime time.Duration) (int, gin.H) {
	smContext := smf_context.GetSMContextByRef(smContextRef)
	if smContext == nil {
		return http.StatusNotFound, gin.H{"error": "no PDU session with this smContextRef"}
	}

	smContext.SMLock.Lock()
	defer smContext.SMLock.Unlock()

	mode := smContext.SelectedSSCMode
	body := gin.H{
		"smContextRef": smContextRef,
		"supi":         smContext.Supi,
		"pduSessionId": smContext.PDUSessionID,
		"sscMode":      smf_context.SSCModeString(mode),
	}
	procedure, status, reason := relocationDecision(smContext.State(), mode, force)
	if procedure != "" && smContext.SSC3Relocation != nil {
		procedure, status, reason = "", http.StatusConflict,
			"an SSC mode 3 relocation of this PDU session is already in progress ("+
				smContext.SSC3Relocation.Phase()+")"
	}
	if procedure == "" {
		body["error"] = reason
		smContext.Log.Warnf("PSA relocation refused: %s", reason)
		return status, body
	}

	oldPSA := ""
	if smContext.SelectedUPF != nil {
		oldPSA = smContext.SelectedUPF.Name
	}
	body["procedure"] = procedure
	body["oldPsa"] = oldPSA
	smContext.Log.Infof("PSA relocation: SSC mode %d, old PSA %s, %s (force=%v)", mode, oldPSA, procedure, force)

	// The UE re-establishes with a new PDU session ID; the hint makes UPF
	// selection for that session try the old PSA last.
	if oldPSA != "" {
		smf_context.SetRelocationHint(smContext.Supi, smContext.Dnn,
			&smf_context.SNssai{Sst: smContext.SNssai.Sst, Sd: smContext.SNssai.Sd}, oldPSA)
	}

	if procedure == relocationModification39 {
		if lifetime <= 0 {
			lifetime = smf_context.DefaultPDUSessionAddressLifetime
		}
		body["lifetime"] = int(lifetime / time.Second)
		if err := p.startSSC3Relocation(smContext, oldPSA, lifetime); err != nil {
			body["error"] = err.Error()
			return http.StatusServiceUnavailable, body
		}
		return http.StatusAccepted, body
	}

	if err := p.releasePDUSessionByNetwork(smContext, nasie.Cause5GSM_ReactivationReq); err != nil {
		body["pfcpError"] = err.Error()
	}
	return http.StatusAccepted, body
}

// releasePDUSessionByNetwork runs the network-requested PDU session release of
// TS 23.502 §4.3.4.2: delete the N4 session(s), end charging, then send the
// Release Command with this cause through Namf_Communication_N1N2MessageTransfer.
// The caller holds SMLock. A PFCP error is returned but does not stop the
// release.
func (p *Processor) releasePDUSessionByNetwork(smContext *smf_context.SMContext, cause uint8) error {
	ctx, cancel := context.WithTimeout(context.Background(), relocationTimeout)
	defer cancel()
	deleted, attempted, err := p.deletePDUSessionPFCPSessions(ctx, smContext)
	smContext.PFCPReleaseDone = err == nil
	if err != nil {
		smContext.Log.Warnf("network-requested release: deleted PFCP sessions %d/%d: %v", deleted, attempted, err)
	}
	p.finalizePDUSessionRelease(smContext, cause)
	return err
}
