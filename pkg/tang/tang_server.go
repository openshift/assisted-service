package tang

import (
	"encoding/json"

	"github.com/pkg/errors"
)

type TangServer struct {
	Url        string `json:"url"`
	Thumbprint string `json:"thumbprint"`
}

func UnmarshalTangServers(tangServersStr string) ([]TangServer, error) {

	var tangServers []TangServer
	if err := json.Unmarshal([]byte(tangServersStr), &tangServers); err != nil {
		return nil, errors.Wrap(err, "Unable to unmarshal tang_servers")
	}
	return tangServers, nil
}
