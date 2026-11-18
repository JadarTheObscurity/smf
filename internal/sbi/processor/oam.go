package processor

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/free5gc/openapi/models"
	"github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/pkg/factory"
	"github.com/free5gc/util/metrics/sbi"
)

type PDUSessionInfo struct {
	Supi         string
	PDUSessionID string
	Dnn          string
	Sst          string
	Sd           string
	AnType       models.AccessType
	PDUAddress   string
	SessionRule  models.Pcf_SMPolCtrl_SessionRule
	UpCnxState   models.Smf_PDUSess_UpCnxState
	Tunnel       context.UPTunnel
	// lab patch 4b: the PSA UPF of this session (smContext.SelectedUPF, the UPF
	// whose pool PDUAddress was allocated from). Omitted when no UPF is selected.
	PsaUpf *PSAUpfInfo `json:",omitempty"`
	// lab patch 6a: the SSC mode selected for this session ("SSC_MODE_1".."3").
	SscMode string `json:",omitempty"`
	// lab patch 6b/6c: SSC mode 3 relocation in progress on this (old) session,
	// and the PDU session ID a new session replaces.
	SscRelocation        *SSC3RelocationInfo `json:",omitempty"`
	ReplacesPduSessionId int32               `json:",omitempty"`
}

// SSC3RelocationInfo is the OAM view of smf_context.SSC3Relocation.
type SSC3RelocationInfo struct {
	Phase           string // "commanded" or "t3593"
	OldPsa          string
	LifetimeSeconds int
	NewPduSessionId int32 `json:",omitempty"`
}

func ssc3RelocationInfo(r *context.SSC3Relocation) *SSC3RelocationInfo {
	if r == nil {
		return nil
	}
	return &SSC3RelocationInfo{
		Phase:           r.Phase(),
		OldPsa:          r.OldPSA,
		LifetimeSeconds: int(r.Lifetime.Seconds()),
		NewPduSessionId: r.NewPduSessionID,
	}
}

// PSAUpfInfo identifies a UPF the same way UPI does: Name is the key used in
// userplaneInformation / UPI upNodesLinks, NodeID is its PFCP Node ID.
type PSAUpfInfo struct {
	Name   string
	NodeID string
}

func psaUpfInfo(upNode *context.UPNode) *PSAUpfInfo {
	if upNode == nil {
		return nil
	}
	return &PSAUpfInfo{Name: upNode.Name, NodeID: upNode.NodeID.String()}
}

func (p *Processor) HandleOAMGetUEPDUSessionInfo(c *gin.Context, smContextRef string) {
	smContext := context.GetSMContextByRef(smContextRef)
	if smContext == nil {
		statusCode := http.StatusNotFound
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(statusCode))
		c.JSON(statusCode, nil)
		return
	}

	pduSessionInfo := &PDUSessionInfo{
		Supi:                 smContext.Supi,
		PDUSessionID:         strconv.Itoa(int(smContext.PDUSessionID)),
		Dnn:                  smContext.Dnn,
		Sst:                  strconv.Itoa(int(smContext.SNssai.Sst)),
		Sd:                   smContext.SNssai.Sd,
		AnType:               smContext.AnType,
		PDUAddress:           smContext.PDUAddress.String(),
		UpCnxState:           smContext.UpCnxState,
		PsaUpf:               psaUpfInfo(smContext.SelectedUPF),
		SscMode:              context.SSCModeString(smContext.SelectedSSCMode),
		SscRelocation:        ssc3RelocationInfo(smContext.SSC3Relocation),
		ReplacesPduSessionId: smContext.ReplacesPDUSessionID,
		// Tunnel: context.UPTunnel{
		// 	//UpfRoot:  smContext.Tunnel.UpfRoot,
		// 	ULCLRoot: smContext.Tunnel.UpfRoot,
		// },
	}
	c.JSON(http.StatusOK, pduSessionInfo)
}

func (p *Processor) HandleGetSMFUserPlaneInfo(c *gin.Context) {
	c.JSON(http.StatusOK, factory.SmfConfig.Configuration.UserPlaneInformation)
}
