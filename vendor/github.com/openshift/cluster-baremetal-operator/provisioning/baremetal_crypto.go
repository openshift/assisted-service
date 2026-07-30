/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provisioning

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/cert"

	"github.com/openshift/library-go/pkg/crypto"
)

type TlsCertificate struct {
	privateKey    []byte
	certificate   []byte
	caCertificate []byte
}

const (
	tlsExpiration = 365 * 24 * time.Hour // 1 year
	tlsRefresh    = 30 * 24 * time.Hour  // 30 days before expiration
)

func generateRandomPassword() (string, error) {
	chars := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789")
	length := 16
	buf := make([]rune, length)
	numChars := big.NewInt(int64(len(chars)))
	for i := range buf {
		c, err := rand.Int(rand.Reader, numChars)
		if err != nil {
			return "", err
		}
		buf[i] = chars[c.Uint64()]
	}
	return string(buf), nil
}

func generateTlsCertificate(hosts sets.Set[string]) (TlsCertificate, error) {
	if hosts.Len() == 0 {
		return TlsCertificate{}, fmt.Errorf("at least one Subject Alternative Name (SAN) host is required for TLS certificate generation")
	}

	caConfig, err := crypto.MakeSelfSignedCAConfig("metal3-ironic", tlsExpiration)
	if err != nil {
		return TlsCertificate{}, err
	}

	ca := crypto.CA{
		Config:          caConfig,
		SerialGenerator: &crypto.RandomSerialGenerator{},
	}

	config, err := ca.MakeServerCert(hosts, tlsExpiration)
	if err != nil {
		return TlsCertificate{}, err
	}

	certBytes, keyBytes, err := config.GetPEMBytes()
	if err != nil {
		return TlsCertificate{}, err
	}

	caCertBytes, _, err := ca.Config.GetPEMBytes()
	if err != nil {
		return TlsCertificate{}, err
	}

	return TlsCertificate{
		privateKey:    keyBytes,
		certificate:   certBytes,
		caCertificate: caCertBytes,
	}, nil
}

func isTlsCertificateExpired(certificate []byte) (bool, error) {
	return isTlsCertificateExpiredAt(certificate, time.Now())
}

func isTlsCertificateExpiredAt(certificate []byte, now time.Time) (bool, error) {
	certs, err := cert.ParseCertsPEM(certificate)
	if err != nil {
		return false, err
	}

	refreshAfter := now.Add(tlsRefresh)
	for _, cert := range certs {
		if !cert.NotAfter.After(refreshAfter) {
			return true, nil
		}
	}

	return false, nil
}

// tlsCertificateSANsMatch checks whether the SANs in the existing certificate
// match the expected hosts. Returns false if they differ (certificate is stale).
func tlsCertificateSANsMatch(certificate []byte, expectedHosts sets.Set[string]) (bool, error) {
	certs, err := cert.ParseCertsPEM(certificate)
	if err != nil {
		return false, err
	}
	if len(certs) == 0 {
		return false, nil
	}

	serverCert := certs[0]
	certHosts := sets.New[string]()
	for _, name := range serverCert.DNSNames {
		certHosts.Insert(name)
	}
	for _, ip := range serverCert.IPAddresses {
		certHosts.Insert(ip.String())
	}

	return certHosts.Equal(expectedHosts), nil
}
