package printer

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eclipse/paho.mqtt.golang/packets"
)

func TestConnectHint(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w : %w", packets.ErrorRefusedNotAuthorised, errors.New("x")), "access code"},
		{packets.ErrorRefusedBadUsernameOrPassword, "access code"},
		{fmt.Errorf("%w : %w", packets.ErrorNetworkError, errors.New("dial tcp: i/o timeout")), "port 8883"},
	} {
		if got := connectHint(tc.err); !strings.Contains(got, tc.want) {
			t.Errorf("connectHint(%v) = %q, want it to mention %q", tc.err, got, tc.want)
		}
	}
}
