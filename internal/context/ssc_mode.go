package context

import (
	"fmt"
	"slices"

	nasie "github.com/free5gc/nas/ie"
	"github.com/free5gc/openapi/models"
)

// lab patch 6a: SSC mode selection.
//
// The upstream SMF ignored the SSC mode the UE requested and the SSC modes in
// the subscription, and always answered SSC mode 1. SelectSSCMode makes the
// choice per PDU session, which later patches (SSC mode 3 PSA relocation) rely
// on: only a session whose selected mode is 3 may be relocated make-before-break.
//
// Rules, TS 23.501 V19.9.0 §5.6.9.3 and TS 24.501 V19.8.0 §6.4.1.3/§6.4.1.4:
//   - Allowed modes = the subscribed default plus the subscribed allowed modes
//     (TS 29.503 SscModes.allowedSscModes are "additional" modes), per DNN and
//     S-NSSAI. Without a subscription the SMF configuration applies, which is
//     SSC mode 1 only (what this SMF has always used).
//   - IPv4/IPv6/IPv4v6 sessions may use mode 1, 2 or 3; Ethernet and
//     Unstructured sessions only 1 or 2.
//   - A session with a subscribed static IP address gets SSC mode 1.
//   - The UE requested an allowed mode: accept it. The UE requested a mode that
//     is not allowed: reject with #68 "not supported SSC mode" and the allowed
//     modes in the Allowed SSC mode IE (the SMF may not pick another mode).
//   - The UE did not request a mode: use the subscribed default.

// ErrSSCModeNotSupported is returned by SelectSSCMode when the requested SSC
// mode is not allowed. The caller rejects the establishment with
// #68 "not supported SSC mode"; smContext.AllowedSSCModes holds the modes to
// put in the Allowed SSC mode IE.
type ErrSSCModeNotSupported struct {
	Requested uint8
	Allowed   []uint8
}

func (e *ErrSSCModeNotSupported) Error() string {
	return fmt.Sprintf("SSC mode %d not supported, allowed %v", e.Requested, e.Allowed)
}

// sscModeFromModels converts a subscription SscMode to the NAS value
// (nasie.SSCMODE1..3). It returns 0 for an empty or unknown value.
func sscModeFromModels(m models.SscMode) uint8 {
	switch m {
	case models.SscMode_1:
		return nasie.SSCMODE1
	case models.SscMode_2:
		return nasie.SSCMODE2
	case models.SscMode_3:
		return nasie.SSCMODE3
	}
	return 0
}

// SSCModeString returns "SSC_MODE_<n>" (the TS 29.503 spelling) for a NAS SSC
// mode value, or "" for 0.
func SSCModeString(mode uint8) string {
	switch mode {
	case nasie.SSCMODE1:
		return string(models.SscMode_1)
	case nasie.SSCMODE2:
		return string(models.SscMode_2)
	case nasie.SSCMODE3:
		return string(models.SscMode_3)
	}
	return ""
}

// requestedSSCModeValue maps the SSC mode value of the request to 1..3, or 0
// when it carries no usable value. TS 24.501 table 9.11.4.16.1: 4, 5 and 6
// "shall be interpreted as" 1, 2 and 3 by the network; 0 and 7 are reserved,
// which is treated like an absent IE.
func requestedSSCModeValue(v uint8) uint8 {
	switch v {
	case 1, 2, 3:
		return v
	case 4, 5, 6:
		return v - 3
	}
	return 0
}

// sscModesForPDUSessionType returns the SSC modes a PDU session type may use.
func sscModesForPDUSessionType(pduSessionType uint8) []uint8 {
	switch pduSessionType {
	case nasie.PDUSessType_Ethernet, nasie.PDUSessType_Unstructured:
		return []uint8{nasie.SSCMODE1, nasie.SSCMODE2}
	}
	return []uint8{nasie.SSCMODE1, nasie.SSCMODE2, nasie.SSCMODE3}
}

// allowedSSCModes returns the default and the allowed SSC modes for this
// session (sorted, default included), applying the subscription, the PDU
// session type and the static IP rule.
func (smContext *SMContext) allowedSSCModes() (def uint8, allowed []uint8) {
	var subscribed []uint8
	if sub := smContext.DnnConfiguration.SscModes; sub != nil {
		def = sscModeFromModels(sub.DefaultSscMode)
		if def != 0 {
			subscribed = append(subscribed, def)
		}
		for _, m := range sub.AllowedSscModes {
			if v := sscModeFromModels(m); v != 0 && !slices.Contains(subscribed, v) {
				subscribed = append(subscribed, v)
			}
		}
	}
	if len(subscribed) == 0 {
		// No usable subscription: SMF configuration, SSC mode 1.
		def, subscribed = nasie.SSCMODE1, []uint8{nasie.SSCMODE1}
	}

	permitted := sscModesForPDUSessionType(smContext.SelectedPDUSessionType)
	if len(smContext.DnnConfiguration.StaticIpAddress) > 0 {
		permitted = []uint8{nasie.SSCMODE1}
	}
	for _, m := range subscribed {
		if slices.Contains(permitted, m) {
			allowed = append(allowed, m)
		}
	}
	slices.Sort(allowed)
	if len(allowed) == 0 {
		// Nothing subscribed fits (e.g. only mode 3 for an Ethernet session):
		// fall back to mode 1, which every UE supports (TS 24.501 §6.4.1.2).
		allowed = []uint8{nasie.SSCMODE1}
	}
	if !slices.Contains(allowed, def) {
		def = allowed[0]
	}
	return def, allowed
}

// SelectSSCMode sets RequestedSSCMode, AllowedSSCModes and SelectedSSCMode for
// this PDU session. requested is the SSC mode value of the PDU Session
// Establishment Request, or 0 when the UE did not include the IE. It returns
// *ErrSSCModeNotSupported when the request has to be rejected with #68.
// Call it after the PDU session type is selected.
func (smContext *SMContext) SelectSSCMode(requested uint8) error {
	def, allowed := smContext.allowedSSCModes()
	requested = requestedSSCModeValue(requested)
	smContext.RequestedSSCMode = requested
	smContext.AllowedSSCModes = allowed
	smContext.SelectedSSCMode = 0

	switch {
	case requested == 0:
		smContext.SelectedSSCMode = def
	case slices.Contains(allowed, requested):
		smContext.SelectedSSCMode = requested
	default:
		smContext.Log.Warnf("SSC mode %d requested, DNN[%s] allows %v: rejecting with #68",
			requested, smContext.Dnn, allowed)
		return &ErrSSCModeNotSupported{Requested: requested, Allowed: allowed}
	}
	smContext.Log.Infof("SSC mode: requested %d, default %d allowed %v, selected %d",
		requested, def, allowed, smContext.SelectedSSCMode)
	return nil
}

// allowedSSCModeIE encodes the Allowed SSC mode IE (TS 24.501 §9.11.4.5, type 1,
// IEI 0xF-) of the PDU Session Establishment Reject: bit 1 SSC1, bit 2 SSC2,
// bit 3 SSC3. nas v1.3.0 leaves ie.AllowedSSCMode.MarshalBinary unimplemented,
// so BuildGSMPDUSessionEstablishmentReject appends this byte itself.
func allowedSSCModeIE(allowed []uint8) byte {
	b := byte(0xF0)
	for _, m := range allowed {
		if m >= nasie.SSCMODE1 && m <= nasie.SSCMODE3 {
			b |= 1 << (m - 1)
		}
	}
	return b
}
