package processor

import (
	"testing"

	"github.com/stretchr/testify/require"

	nasie "github.com/free5gc/nas/ie"
	"github.com/free5gc/nas/message"
	"github.com/free5gc/openapi/models"
	smf_context "github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/logger"
)

// lab patch 6a: the SSC mode IE of the Establishment Request, decoded from the
// NAS bytes the UE sends, decides the selected SSC mode.
func TestHandlePDUSessionEstablishmentRequestSelectsSSCMode(t *testing.T) {
	cases := []struct {
		name string
		ie   *nasie.SSCMode
		want uint8
	}{
		{"UE requests mode 3", &nasie.SSCMode{Mode: nasie.SSCMODE3}, nasie.SSCMODE3},
		{"UE requests mode 2", &nasie.SSCMode{Mode: nasie.SSCMODE2}, nasie.SSCMODE2},
		{"UE requests mode 1", &nasie.SSCMode{Mode: nasie.SSCMODE1}, nasie.SSCMODE1},
		{"no SSC mode IE", nil, nasie.SSCMODE1},
	}
	// The PDU session type IE is absent in these requests, so the handler uses
	// the SMF's supported type (IPv4 by default) before selecting the SSC mode.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := (&message.PDUSessEstReq{
				PDUSessId: 1,
				PTI:       1,
				IntegrityProtectionMaxDataRate: &nasie.IntegrityProtectionMaxDataRate{
					Uplink: 0xff, Downlink: 0xff,
				},
				SSCMode: tc.ie,
			}).MarshalBinary()
			require.NoError(t, err)
			req := new(message.PDUSessEstReq)
			require.NoError(t, req.UnmarshalBinary(b))

			smCtx := &smf_context.SMContext{
				Smf_PDUSess_SmContextCreateData: &models.Smf_PDUSess_SmContextCreateData{Dnn: "internet"},
				DnnConfiguration: models.Udm_SDM_DnnConfiguration{SscModes: &models.Udm_SDM_SscModes{
					DefaultSscMode:  models.SscMode_1,
					AllowedSscModes: []models.SscMode{models.SscMode_2, models.SscMode_3},
				}},
				Log: logger.PduSessLog,
			}
			require.NoError(t, HandlePDUSessionEstablishmentRequest(smCtx, req))
			require.Equal(t, []uint8{1, 2, 3}, smCtx.AllowedSSCModes)
			require.Equal(t, tc.want, smCtx.SelectedSSCMode)
			require.Equal(t, smf_context.SSCModeString(tc.want),
				smf_context.SSCModeString(smCtx.SelectedSSCMode))
		})
	}
}

// A mode outside the subscription makes the handler return #68.
func TestHandlePDUSessionEstablishmentRequestRejectsSSCMode(t *testing.T) {
	b, err := (&message.PDUSessEstReq{
		PDUSessId: 1,
		PTI:       1,
		IntegrityProtectionMaxDataRate: &nasie.IntegrityProtectionMaxDataRate{
			Uplink: 0xff, Downlink: 0xff,
		},
		SSCMode: &nasie.SSCMode{Mode: nasie.SSCMODE3},
	}).MarshalBinary()
	require.NoError(t, err)
	req := new(message.PDUSessEstReq)
	require.NoError(t, req.UnmarshalBinary(b))

	smCtx := &smf_context.SMContext{
		Smf_PDUSess_SmContextCreateData: &models.Smf_PDUSess_SmContextCreateData{Dnn: "internet"},
		DnnConfiguration: models.Udm_SDM_DnnConfiguration{SscModes: &models.Udm_SDM_SscModes{
			DefaultSscMode: models.SscMode_1,
		}},
		Log: logger.PduSessLog,
	}
	err = HandlePDUSessionEstablishmentRequest(smCtx, req)
	gsmErr, ok := err.(*GSMError)
	require.True(t, ok, "want *GSMError, got %v", err)
	require.Equal(t, nasie.Cause5GSM_NotSupportedSSCMode, gsmErr.GSMCause)
	require.Equal(t, []uint8{1}, smCtx.AllowedSSCModes)
}
