package gencrypto

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"
)

var _ = Describe("SSHKeyPair", func() {
	It("generates an ECDSA P-256 keypair", func() {
		pub, _, err := SSHKeyPair()
		Expect(err).NotTo(HaveOccurred())

		publicKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub))
		Expect(err).NotTo(HaveOccurred())
		Expect(publicKey.Type()).To(Equal("ecdsa-sha2-nistp256"))
	})
})
