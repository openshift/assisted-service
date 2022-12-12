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
	"math/big"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/cert"

	"github.com/openshift/library-go/pkg/crypto"
)

type TlsCertificate struct {
	privateKey  []byte
	certificate []byte
}

const (
	tlsExpirationDays = 365 * 2
	tlsRefreshDays    = 180
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

func generateTlsCertificate(provisioningIP string) (TlsCertificate, error) {
	caConfig, err := crypto.MakeSelfSignedCAConfig("metal3-ironic", tlsExpirationDays)
	if err != nil {
		return TlsCertificate{}, err
	}

	ca := crypto.CA{
		Config:          caConfig,
		SerialGenerator: &crypto.RandomSerialGenerator{},
	}

	var host string
	if provisioningIP == "" {
		host = "localhost"
	} else {
		host = provisioningIP
	}

	config, err := ca.MakeServerCert(sets.NewString(host), tlsExpirationDays)
	if err != nil {
		return TlsCertificate{}, err
	}

	certBytes, keyBytes, err := config.GetPEMBytes()
	if err != nil {
		return TlsCertificate{}, err
	}

	return TlsCertificate{
		privateKey:  keyBytes,
		certificate: certBytes,
	}, nil
}

func isTlsCertificateExpired(certificate []byte) (bool, error) {
	certs, err := cert.ParseCertsPEM(certificate)
	if err != nil {
		return false, err
	}

	refreshAfter := time.Now().AddDate(0, 0, tlsRefreshDays)
	for _, cert := range certs {
		if cert.NotAfter.Before(refreshAfter) {
			return true, nil
		}
	}

	return false, nil
}
