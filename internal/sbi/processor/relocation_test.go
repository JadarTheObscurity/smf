package processor

import (
	"net/http"
	"testing"

	nasie "github.com/free5gc/nas/ie"
	smf_context "github.com/free5gc/smf/internal/context"
)

func TestRelocationDecision(t *testing.T) {
	cases := []struct {
		name      string
		state     smf_context.SMContextState
		mode      uint8
		force     bool
		procedure string
		status    int
	}{
		{"mode 2 active", smf_context.Active, nasie.SSCMODE2, false, relocationRelease39, http.StatusAccepted},
		{"mode 2 force", smf_context.Active, nasie.SSCMODE2, true, relocationRelease39, http.StatusAccepted},
		{"mode 1 refused", smf_context.Active, nasie.SSCMODE1, false, "", http.StatusConflict},
		{"mode 1 forced", smf_context.Active, nasie.SSCMODE1, true, relocationRelease39, http.StatusAccepted},
		{"pre-6a session counts as mode 1", smf_context.Active, 0, false, "", http.StatusConflict},
		{"mode 3 not yet", smf_context.Active, nasie.SSCMODE3, false, "", http.StatusNotImplemented},
		{"mode 3 force does not help", smf_context.Active, nasie.SSCMODE3, true, "", http.StatusNotImplemented},
		{"not active", smf_context.ActivePending, nasie.SSCMODE2, false, "", http.StatusConflict},
		{"releasing", smf_context.InActivePending, nasie.SSCMODE2, true, "", http.StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			procedure, status, reason := relocationDecision(tc.state, tc.mode, tc.force)
			if procedure != tc.procedure || status != tc.status {
				t.Fatalf("got %q %d (%s), want %q %d", procedure, status, reason, tc.procedure, tc.status)
			}
			if procedure == "" && reason == "" {
				t.Fatal("a refusal needs a reason")
			}
		})
	}
}
