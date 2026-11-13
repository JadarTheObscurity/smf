package processor

import (
	smf_context "github.com/free5gc/smf/internal/context"
)

// lab patch 4a: Nudm_UECM registration of the SMF for a PDU Session.

// registerWithUDM registers this SMF with the UDM for the PDU Session.
// Errors are logged and do not fail the establishment.
func (p *Processor) registerWithUDM(smContext *smf_context.SMContext) {
	problemDetails, err := p.Consumer().UeCmRegistration(smContext)
	switch {
	case problemDetails != nil:
		smContext.Log.Errorf("UECM_Registration Failed Problem[%+v]", problemDetails)
	case err != nil:
		smContext.Log.Errorf("UECM_Registration Error[%+v]", err)
	default:
		smContext.Log.Infof("UECM_Registration successful (SUPI[%s] PDUSessionID[%d])",
			smContext.Supi, smContext.PDUSessionID)
	}
}

// deregisterFromUDM removes the registration if it is still in place.
func (p *Processor) deregisterFromUDM(smContext *smf_context.SMContext) {
	if !smContext.UeCmRegistered {
		return
	}
	problemDetails, err := p.Consumer().UeCmDeregistration(smContext)
	switch {
	case problemDetails != nil:
		if problemDetails.Cause != CONTEXT_NOT_FOUND {
			smContext.Log.Errorf("UECM_DeRegistration Failed Problem[%+v]", problemDetails)
		}
	case err != nil:
		smContext.Log.Errorf("UECM_DeRegistration Error[%+v]", err)
	default:
		smContext.Log.Infof("UECM_DeRegistration successful (SUPI[%s] PDUSessionID[%d])",
			smContext.Supi, smContext.PDUSessionID)
	}
}
