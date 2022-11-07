package dns

import "github.com/danielerez/go-dns-client/pkg/dnsproviders"

//go:generate mockgen -source=mocks.go -package=dns -destination=mock_dns_vendor.generated_go
type DNSProvider interface {
	dnsproviders.Provider
}
