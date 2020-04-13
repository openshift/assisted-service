package hardware

import "github.com/filanov/bm-inventory/models"

type IsSufficientReply struct {
	IsSufficient bool
	Reason       string
}

//go:generate mockgen -source=validator.go -package=hardware -destination=mock_validator.go
type Validator interface {
	IsSufficient(host *models.Host) (*IsSufficientReply, error)
}

func NewValidator() Validator {
	return &validator{}
}

type validator struct{}

func (v validator) IsSufficient(host *models.Host) (*IsSufficientReply, error) {
	// TODO: if host have a role need to validate requirements by role, else validate minimal requirements
	return &IsSufficientReply{
		IsSufficient: true,
	}, nil
}
