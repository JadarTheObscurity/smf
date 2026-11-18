package context

import (
	"fmt"
	"sync"
	"time"

	"github.com/free5gc/smf/internal/logger"
)

// lab patch 5: PSA relocation hints.
//
// When the SMF releases a PDU session to move it to another PSA (SSC mode 2,
// TS 23.502 §4.3.5.1, cause #39), the UE re-establishes it with a new PDU
// session ID, which the SMF sees as an unrelated new session. A relocation
// hint remembers, per SUPI, DNN and S-NSSAI, which UPF the released session was
// anchored on, so UPF selection for the next session of that UE to that DN
// tries the old PSA last. Without it the LCI ordering could pick the same UPF
// again. A hint is used once and expires after RelocationHintTTL.

// RelocationHintTTL is how long a relocation hint stays valid. The UE answers a
// Release Command #39 within seconds; UERANSIM re-establishes at once.
const RelocationHintTTL = 60 * time.Second

type relocationHint struct {
	avoidUPF string
	expires  time.Time
}

var (
	relocationHintsMu sync.Mutex
	relocationHints   = map[string]relocationHint{}
	relocationNow     = time.Now
)

func relocationKey(supi, dnn string, snssai *SNssai) string {
	if snssai == nil {
		return fmt.Sprintf("%s|%s|-", supi, dnn)
	}
	return fmt.Sprintf("%s|%s|%d-%s", supi, dnn, snssai.Sst, snssai.Sd)
}

// SetRelocationHint records that the next PDU session of this UE to this DNN
// and S-NSSAI should avoid avoidUPF (a UPF name as in userplaneInformation).
func SetRelocationHint(supi, dnn string, snssai *SNssai, avoidUPF string) {
	relocationHintsMu.Lock()
	defer relocationHintsMu.Unlock()
	relocationHints[relocationKey(supi, dnn, snssai)] = relocationHint{
		avoidUPF: avoidUPF,
		expires:  relocationNow().Add(RelocationHintTTL),
	}
}

// TakeRelocationHint returns and removes the UPF to avoid for this UE, DNN and
// S-NSSAI, if a hint is pending and not expired.
func TakeRelocationHint(supi, dnn string, snssai *SNssai) (string, bool) {
	relocationHintsMu.Lock()
	defer relocationHintsMu.Unlock()
	key := relocationKey(supi, dnn, snssai)
	h, ok := relocationHints[key]
	if !ok {
		return "", false
	}
	delete(relocationHints, key)
	if relocationNow().After(h.expires) {
		return "", false
	}
	return h.avoidUPF, true
}

// deprioritizeUPF moves the UPF called avoid to the end of the try order. It is
// kept as a last resort (the only UPF left, or the others are unavailable or
// out of addresses) rather than excluded.
func (upi *UserPlaneInformation) deprioritizeUPF(list []*UPNode, avoid string) []*UPNode {
	if avoid == "" {
		return list
	}
	out := make([]*UPNode, 0, len(list))
	var last []*UPNode
	for _, node := range list {
		if node.Name == avoid {
			last = append(last, node)
			continue
		}
		out = append(out, node)
	}
	if len(last) > 0 {
		logger.CtxLog.Infof("UPF selection: relocation, trying old PSA %s last", avoid)
	}
	return append(out, last...)
}
