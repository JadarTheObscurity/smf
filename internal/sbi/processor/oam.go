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
		Supi:         smContext.Supi,
		PDUSessionID: strconv.Itoa(int(smContext.PDUSessionID)),
		Dnn:          smContext.Dnn,
		Sst:          strconv.Itoa(int(smContext.SNssai.Sst)),
		Sd:           smContext.SNssai.Sd,
		AnType:       smContext.AnType,
		PDUAddress:   smContext.PDUAddress.String(),
		UpCnxState:   smContext.UpCnxState,
		PsaUpf:       psaUpfInfo(smContext.SelectedUPF),
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
