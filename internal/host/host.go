package host

import (
	"context"
	"fmt"

	"github.com/filanov/bm-inventory/internal/hardware"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
)

//go:generate mockgen -source=statemachine.go -package=host -destination=mock_host_api.go
type API interface {
	// Register a new host
	RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error)
	// Set a new HW information
	UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error)
	// Set host state
	UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error)
	// check keep alive
	RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error)
	// Install host - db is optional, for transactions
	Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error)
	// Enable host to get requests (disabled by default)
	EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error)
	// Disable host from getting any requests
	DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error)
}

type State struct {
	discovering  API
	known        API
	insufficient API
	disconnected API
	disabled     API
	installing   API
	installed    API
	error        API
}

func NewState(log logrus.FieldLogger, db *gorm.DB, hwValidator hardware.Validator) *State {
	return &State{
		discovering:  NewDiscoveringState(log, db, hwValidator),
		known:        NewKnownState(log, db, hwValidator),
		insufficient: NewInsufficientState(log, db, hwValidator),
		disconnected: NewDisconnectedState(log, db, hwValidator),
		disabled:     NewDisabledState(log, db),
		installing:   NewInstallingState(log, db),
		installed:    NewInstalledState(log, db),
		error:        NewErrorState(log, db),
	}
}

func (s *State) getCurrentState(status string) (API, error) {
	switch status {
	case "":
	case hostStatusDiscovering:
		return s.discovering, nil
	case hostStatusKnown:
		return s.known, nil
	case hostStatusInsufficient:
		return s.insufficient, nil
	case hostStatusDisconnected:
		return s.disconnected, nil
	case hostStatusDisabled:
		return s.disabled, nil
	case hostStatusInstalling:
		return s.installing, nil
	case hostStatusInstalled:
		return s.installed, nil
	case hostStatusError:
		return s.error, nil
	}
	return nil, fmt.Errorf("not supported host status: %s", status)
}

func (s *State) RegisterHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.RegisterHost(ctx, h)
}

func (s *State) UpdateHwInfo(ctx context.Context, h *models.Host, hwInfo string) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateHwInfo(ctx, h, hwInfo)
}

func (s *State) UpdateRole(ctx context.Context, h *models.Host, role string, db *gorm.DB) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.UpdateRole(ctx, h, role, db)
}

func (s *State) RefreshStatus(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.RefreshStatus(ctx, h)
}

func (s *State) Install(ctx context.Context, h *models.Host, db *gorm.DB) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.Install(ctx, h, db)
}

func (s *State) EnableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.EnableHost(ctx, h)
}

func (s *State) DisableHost(ctx context.Context, h *models.Host) (*UpdateReply, error) {
	state, err := s.getCurrentState(swag.StringValue(h.Status))
	if err != nil {
		return nil, err
	}
	return state.DisableHost(ctx, h)
}
