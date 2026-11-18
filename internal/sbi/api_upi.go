package sbi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	smf_context "github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/logger"
	"github.com/free5gc/smf/pkg/factory"
	"github.com/free5gc/util/metrics/sbi"
)

func (s *Server) getUPIRoutes() []Route {
	return []Route{
		{
			Name:    "Index",
			Method:  http.MethodGet,
			Pattern: "/",
			APIFunc: func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"status": "Service Available"})
			},
		},
		{
			Name:    "GetUpNodesLinks",
			Method:  http.MethodGet,
			Pattern: "/upNodesLinks",
			APIFunc: s.GetUpNodesLinks,
		},
		{
			Name:    "AddUpNodesLinks",
			Method:  http.MethodPost,
			Pattern: "/upNodesLinks",
			APIFunc: s.PostUpNodesLinks,
		},
		{
			Name:    "DeleteUpNodeLink",
			Method:  http.MethodDelete,
			Pattern: "/upNodesLinks/:upNodeRef",
			APIFunc: s.DeleteUpNodeLink,
		},
		{
			Name:    "GetLoadControl",
			Method:  http.MethodGet,
			Pattern: "/lci",
			APIFunc: s.GetLoadControl,
		},
		{
			// lab patch 5: network-triggered PSA relocation of one PDU session
			Name:    "RelocatePDUSession",
			Method:  http.MethodPost,
			Pattern: "/sessions/:smContextRef/relocate",
			APIFunc: s.RelocatePDUSession,
		},
		{
			Name:    "PutUpNodeLoadControl",
			Method:  http.MethodPut,
			Pattern: "/upNodesLinks/:upNodeRef/lci",
			APIFunc: s.PutUpNodeLoadControl,
		},
	}
}

// RelocatePDUSession moves one PDU session to another PSA, by the procedure its
// SSC mode allows (lab patch 5, 6b). Query force=true also relocates an SSC
// mode 1 session; lifetime=<s> sets the PDU session address lifetime for SSC
// mode 3. 202: the command was sent; 400: bad lifetime; 404: no such session;
// 409: not Active, SSC mode 1 without force, or a relocation already in
// progress; 503: the AMF did not take the SSC mode 3 Modification Command.
func (s *Server) RelocatePDUSession(c *gin.Context) {
	force := c.Query("force") == "true"
	// lab patch 6b: PDU session address lifetime for SSC mode 3, in seconds
	var lifetime time.Duration
	if v := c.Query("lifetime"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs < 1 || secs > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "lifetime must be 1..65535 seconds"})
			return
		}
		lifetime = time.Duration(secs) * time.Second
	}
	status, body := s.Processor().RelocatePDUSession(c.Params.ByName("smContextRef"), force, lifetime)
	if status >= http.StatusBadRequest {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(status))
	}
	c.JSON(status, body)
}

// GetLoadControl lists, per UPF, the LCI used for UPF selection, the active
// session count and the PFCP association state.
func (s *Server) GetLoadControl(c *gin.Context) {
	upi := smf_context.GetSelf().UserPlaneInformation
	upi.Mu.RLock()
	defer upi.Mu.RUnlock()

	out := gin.H{}
	for name, node := range upi.UPFs {
		out[name] = loadControlEntry(node)
	}
	c.JSON(http.StatusOK, out)
}

// loadControlEntry is one UPF in GET /upi/v1/lci.
// lab patch 4c: "association" is the PFCP association state (down, setting-up,
// established, releasing); "associationGeneration" increases whenever the
// association is established or torn down (heartbeat loss, changed Recovery
// Time Stamp, release), so a different value than the last one seen means the
// UPF re-associated (e.g. it restarted), its PFCP sessions were released and
// its LCI was cleared.
func loadControlEntry(node *smf_context.UPNode) gin.H {
	state, generation := node.UPF.AssociationStateAndGeneration()
	entry := gin.H{
		"nodeID":                node.UPF.NodeID.ResolveNodeIdToIp().String(),
		"sessions":              node.ActiveSessions(),
		"association":           state.String(),
		"associationGeneration": generation,
	}
	if lc, ok := node.UPF.LoadControl(); ok {
		entry["lci"] = lc
	}
	return entry
}

// PutUpNodeLoadControl injects a Load Control Information for one UPF, with the
// same rules as an LCI received over PFCP (lab: simulated LCI, TS 29.244 §6.2.3).
func (s *Server) PutUpNodeLoadControl(c *gin.Context) {
	var req struct {
		SequenceNumber *uint32 `json:"sequenceNumber" binding:"required"`
		Metric         *uint8  `json:"metric" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(http.StatusBadRequest))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	upNodeRef := c.Params.ByName("upNodeRef")
	upi := smf_context.GetSelf().UserPlaneInformation
	upi.Mu.RLock()
	upNode, ok := upi.UPNodes[upNodeRef]
	upi.Mu.RUnlock()
	if !ok || upNode.Type != smf_context.UPNODE_UPF || upNode.UPF == nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(http.StatusNotFound))
		c.JSON(http.StatusNotFound, gin.H{"error": "no UPF named " + upNodeRef})
		return
	}

	accepted, err := upNode.UPF.UpdateLoadControl(*req.SequenceNumber, *req.Metric, smf_context.LoadControlSourceUPI)
	if err != nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(http.StatusBadRequest))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	current, _ := upNode.UPF.LoadControl()
	if !accepted {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(http.StatusConflict))
		c.JSON(http.StatusConflict, gin.H{
			"accepted": false, "error": "sequenceNumber is not higher than the stored one", "lci": current,
		})
		return
	}
	logger.SBILog.Infof("UPF[%s] Load Control Information: sequence %d, metric %d (upi)",
		upNodeRef, current.SequenceNumber, current.Metric)
	c.JSON(http.StatusOK, gin.H{"accepted": true, "lci": current})
}

func (s *Server) GetUpNodesLinks(c *gin.Context) {
	upi := smf_context.GetSelf().UserPlaneInformation
	upi.Mu.RLock()
	defer upi.Mu.RUnlock()

	nodes := upi.UpNodesToConfiguration()
	links := upi.LinksToConfiguration()

	json := &factory.UserPlaneInformation{
		UPNodes: nodes,
		Links:   links,
	}

	c.JSON(http.StatusOK, json)
}

func (s *Server) PostUpNodesLinks(c *gin.Context) {
	upi := smf_context.GetSelf().UserPlaneInformation
	upi.Mu.Lock()
	defer upi.Mu.Unlock()

	var json factory.UserPlaneInformation
	if err := c.ShouldBindJSON(&json); err != nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(int(http.StatusBadRequest)))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := upi.UpNodesFromConfiguration(&json); err != nil {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(int(http.StatusBadRequest)))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	upi.LinksFromConfiguration(&json)

	for _, upf := range upi.UPFs {
		// only associate new ones
		if err := upf.UPF.IsAssociated(); err != nil {
			go s.Processor().ToBeAssociatedWithUPF(smf_context.GetSelf().PfcpContext, upf.UPF)
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "OK"})
}

func (s *Server) DeleteUpNodeLink(c *gin.Context) {
	// current version does not allow node deletions when ulcl is enabled
	if smf_context.GetSelf().ULCLSupport {
		c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(int(http.StatusForbidden)))
		c.JSON(http.StatusForbidden, gin.H{})
	} else {
		upNodeRef := c.Params.ByName("upNodeRef")
		upi := smf_context.GetSelf().UserPlaneInformation
		upi.Mu.Lock()
		defer upi.Mu.Unlock()
		if upNode, ok := upi.UPNodes[upNodeRef]; ok {
			if upNode.Type == smf_context.UPNODE_UPF && upNode.UPF != nil {
				go s.Processor().ReleaseAllResourcesOfUPF(upNode.UPF)
				// Mark removed before canceling so the lifecycle owner sees it
				// and does not set up a new association (UpNodeDelete marks
				// it again, idempotently).
				upNode.UPF.MarkRemoved()
				upNode.UPF.CancelAssociation()
			}
			upi.UpNodeDelete(upNodeRef)
			c.JSON(http.StatusOK, gin.H{"status": "OK"})
		} else {
			statusCode := http.StatusNotFound
			c.Set(sbi.IN_PB_DETAILS_CTX_STR, http.StatusText(statusCode))
			c.JSON(statusCode, gin.H{})
		}
	}
}
