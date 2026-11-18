package context_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	nasie "github.com/free5gc/nas/ie"
	"github.com/free5gc/openapi/models"
	"github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/logger"
)

func newSSCTestContext(sub *models.Udm_SDM_SscModes, pduType uint8, staticIP bool) *context.SMContext {
	// NewSMContext needs the whole SMF context (TEID generator), which
	// SelectSSCMode does not use.
	smContext := &context.SMContext{
		Smf_PDUSess_SmContextCreateData: &models.Smf_PDUSess_SmContextCreateData{Dnn: "internet"},
		DnnConfiguration:                models.Udm_SDM_DnnConfiguration{SscModes: sub},
		SelectedPDUSessionType:          pduType,
		Log:                             logger.PduSessLog,
	}
	if staticIP {
		smContext.DnnConfiguration.StaticIpAddress = []models.Udm_SDM_IpAddress{{Ipv4Addr: "10.60.0.100"}}
	}
	return smContext
}

func TestSelectSSCMode(t *testing.T) {
	// The lab subscription: default 1, allowed 2 and 3.
	lab := &models.Udm_SDM_SscModes{
		DefaultSscMode:  models.SscMode_1,
		AllowedSscModes: []models.SscMode{models.SscMode_2, models.SscMode_3},
	}
	only3 := &models.Udm_SDM_SscModes{DefaultSscMode: models.SscMode_3}
	bogus := &models.Udm_SDM_SscModes{
		DefaultSscMode:  "SSC_MODE_9",
		AllowedSscModes: []models.SscMode{"", models.SscMode_2, models.SscMode_2},
	}
	const (
		ipv4 = nasie.PDUSessType_IPv4
		eth  = nasie.PDUSessType_Ethernet
	)

	cases := []struct {
		name        string
		sub         *models.Udm_SDM_SscModes
		pduType     uint8
		staticIP    bool
		requested   uint8
		want        uint8   // selected mode; 0 = rejected with #68
		wantAllowed []uint8 // AllowedSSCModes
	}{
		{"not requested, lab default", lab, ipv4, false, 0, 1, []uint8{1, 2, 3}},
		{"requested 1", lab, ipv4, false, 1, 1, []uint8{1, 2, 3}},
		{"requested 2", lab, ipv4, false, 2, 2, []uint8{1, 2, 3}},
		{"requested 3", lab, ipv4, false, 3, 3, []uint8{1, 2, 3}},
		{"requested 1, only 3 subscribed -> #68", only3, ipv4, false, 1, 0, []uint8{3}},
		{"not requested, default 3", only3, ipv4, false, 0, 3, []uint8{3}},
		{"no subscription, requested 3 -> #68", nil, ipv4, false, 3, 0, []uint8{1}},
		{"no subscription, not requested -> 1", nil, ipv4, false, 0, 1, []uint8{1}},
		{"unknown default ignored, 2 still allowed", bogus, ipv4, false, 2, 2, []uint8{2}},
		{"unknown default ignored, not requested -> 2", bogus, ipv4, false, 0, 2, []uint8{2}},
		{"Ethernet: mode 3 not allowed -> #68", lab, eth, false, 3, 0, []uint8{1, 2}},
		{"Ethernet: requested 2", lab, eth, false, 2, 2, []uint8{1, 2}},
		{"Ethernet, only 3 subscribed: falls back to 1", only3, eth, false, 0, 1, []uint8{1}},
		{"static IP: requested 3 -> #68", lab, ipv4, true, 3, 0, []uint8{1}},
		{"static IP: not requested -> 1", only3, ipv4, true, 0, 1, []uint8{1}},
		{"value 6 is read as 3", lab, ipv4, false, 6, 3, []uint8{1, 2, 3}},
		{"value 4 is read as 1", lab, ipv4, false, 4, 1, []uint8{1, 2, 3}},
		{"reserved value 7 is like no IE", lab, ipv4, false, 7, 1, []uint8{1, 2, 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			smContext := newSSCTestContext(tc.sub, tc.pduType, tc.staticIP)
			err := smContext.SelectSSCMode(tc.requested)
			require.Equal(t, tc.wantAllowed, smContext.AllowedSSCModes)
			require.Equal(t, tc.want, smContext.SelectedSSCMode)
			if tc.want == 0 {
				var nse *context.ErrSSCModeNotSupported
				require.True(t, errors.As(err, &nse), "want ErrSSCModeNotSupported, got %v", err)
				require.Equal(t, tc.wantAllowed, nse.Allowed)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBuildEstablishmentRejectAllowedSSCMode(t *testing.T) {
	smContext := newSSCTestContext(&models.Udm_SDM_SscModes{
		DefaultSscMode:  models.SscMode_1,
		AllowedSscModes: []models.SscMode{models.SscMode_2},
	}, nasie.PDUSessType_IPv4, false)
	smContext.PDUSessionID = 5
	smContext.Pti = 3
	require.Error(t, smContext.SelectSSCMode(nasie.SSCMODE3))

	// EPD 0x2E, PSI 5, PTI 3, type 0xC3 (Establishment Reject), cause #68,
	// then Allowed SSC mode: IEI 0xF, SSC1 and SSC2 set.
	b, err := context.BuildGSMPDUSessionEstablishmentReject(smContext, nasie.Cause5GSM_NotSupportedSSCMode)
	require.NoError(t, err)
	require.Equal(t, []byte{0x2e, 0x05, 0x03, 0xc3, 0x44, 0xf3}, b)

	// Other causes do not carry the IE.
	b, err = context.BuildGSMPDUSessionEstablishmentReject(smContext, nasie.Cause5GSM_ReqRejected)
	require.NoError(t, err)
	require.Equal(t, []byte{0x2e, 0x05, 0x03, 0xc3, nasie.Cause5GSM_ReqRejected}, b)
}

func TestSSCModeString(t *testing.T) {
	require.Equal(t, "SSC_MODE_1", context.SSCModeString(nasie.SSCMODE1))
	require.Equal(t, "SSC_MODE_3", context.SSCModeString(nasie.SSCMODE3))
	require.Equal(t, "", context.SSCModeString(0))
}
