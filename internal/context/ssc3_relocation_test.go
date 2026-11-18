package context_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nasie "github.com/free5gc/nas/ie"
	"github.com/free5gc/nas/message"
	"github.com/free5gc/openapi/models"
	"github.com/free5gc/smf/internal/context"
	"github.com/free5gc/smf/internal/logger"
)

func TestBuildGSMSSC3RelocationModificationCommand(t *testing.T) {
	smContext := &context.SMContext{
		Smf_PDUSess_SmContextCreateData: &models.Smf_PDUSess_SmContextCreateData{Dnn: "internet"},
		Log:                             logger.PduSessLog,
	}
	smContext.PDUSessionID = 1
	smContext.Pti = 7 // must not be used: network-requested, no PTI

	b, err := context.BuildGSMSSC3RelocationModificationCommand(smContext, 60*time.Second)
	require.NoError(t, err)
	// EPD 0x2E, PSI 1, PTI 0, type 0xCB, Cause IEI 0x59 #39,
	// ePCO IEI 0x7B len 6: 0x80, container 001EH, len 2, 60 s.
	require.Equal(t, []byte{
		0x2e, 0x01, 0x00, 0xcb, 0x59, 0x27,
		0x7b, 0x00, 0x06, 0x80, 0x00, 0x1e, 0x02, 0x00, 0x3c,
	}, b)

	// The NAS library still decodes the message and its cause.
	cmd := new(message.PDUSessModCmd)
	_ = cmd.UnmarshalBinary(b)
	require.NotNil(t, cmd.Cause5GSM)
	require.Equal(t, nasie.Cause5GSM_ReactivationReq, cmd.Cause5GSM.Value)

	// Lifetime bounds: at least 1 s, at most 65535 s.
	b, err = context.BuildGSMSSC3RelocationModificationCommand(smContext, 0)
	require.NoError(t, err)
	require.Equal(t, []byte{0x00, 0x01}, b[len(b)-2:])
	b, err = context.BuildGSMSSC3RelocationModificationCommand(smContext, 100000*time.Second)
	require.NoError(t, err)
	require.Equal(t, []byte{0xff, 0xff}, b[len(b)-2:])
}

func TestSSC3RelocationPhase(t *testing.T) {
	var none *context.SSC3Relocation
	require.Equal(t, "", none.Phase())
	none.StopT3593() // nil-safe

	r := &context.SSC3Relocation{CommandSentAt: time.Now()}
	require.Equal(t, "commanded", r.Phase())
	r.CompletedAt = time.Now()
	require.Equal(t, "t3593", r.Phase())
}

// T3593 uses NewTimer with maxRetryTimes 0: the cancel function runs once,
// after one period (lab patch 6b/6c; with 1 it ran after two periods).
func TestTimerFiresOnceWithZeroRetries(t *testing.T) {
	fired := make(chan time.Time, 2)
	start := time.Now()
	tm := context.NewTimer(50*time.Millisecond, 0, func(int32) { t.Error("expiredFunc must not run") },
		func() { fired <- time.Now() })
	defer tm.Stop()
	select {
	case at := <-fired:
		require.Less(t, at.Sub(start), 90*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("timer did not fire")
	}
}
