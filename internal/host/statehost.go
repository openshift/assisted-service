package host

import (
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/stateswitch"
	"github.com/go-openapi/swag"
)

type stateHost struct {
	srcState string
	host     *models.Host
}

func newStateHost(h *models.Host) *stateHost {
	return &stateHost{
		srcState: swag.StringValue(h.Status),
		host:     h,
	}
}

func (sh *stateHost) State() stateswitch.State {
	return stateswitch.State(swag.StringValue(sh.host.Status))
}

func (sh *stateHost) SetState(state stateswitch.State) error {
	sh.host.Status = swag.String(string(state))
	return nil
}
