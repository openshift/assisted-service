package network

import (
	"fmt"
	"net/url"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type vip struct {
	Name       string `yaml:"name"`
	MacAddress string `yaml:"mac-address"`
	IpAddress  string `yaml:"ip-address"`
}
type vips struct {
	APIVip     *vip `yaml:"api-vip"`
	IngressVip *vip `yaml:"ingress-vip"`
}

func generateOpenshiftDhcpParamFileContents(cluster *common.Cluster) ([]byte, error) {
	if swag.BoolValue(cluster.VipDhcpAllocation) && !swag.BoolValue(cluster.UserManagedNetworking) {
		if GetApiVipById(cluster, 0) != "" && GetIngressVipById(cluster, 0) != "" {
			v := vips{
				APIVip: &vip{
					Name:       "api",
					MacAddress: GenerateAPIVipMAC(cluster.ID.String()),
					IpAddress:  GetApiVipById(cluster, 0),
				},
				IngressVip: &vip{
					Name:       "ingress",
					MacAddress: GenerateIngressVipMAC(cluster.ID.String()),
					IpAddress:  GetIngressVipById(cluster, 0),
				},
			}
			return yaml.Marshal(&v)
		} else {
			return nil, errors.Errorf("Either API VIP <%s> or Ingress VIP <%s> are not set",
				GetApiVipById(cluster, 0), GetIngressVipById(cluster, 0))
		}
	}
	return nil, nil
}

func GetEncodedDhcpParamFileContents(cluster *common.Cluster) (string, error) {
	b, err := generateOpenshiftDhcpParamFileContents(cluster)
	if err != nil || b == nil {
		return "", err
	}
	return fmt.Sprintf("data:,%s", url.PathEscape(string(b))), nil
}
