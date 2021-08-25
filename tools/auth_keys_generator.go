package main

import (
	"crypto"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dgrijalva/jwt-go"
	keygen_tools "github.com/openshift/assisted-service/pkg/auth"
)

func main() {
	var keysDir string
	flag.StringVar(&keysDir, "keys-dir", "../build", "directory path for generates keys and token. defaults to build directory")
	flag.Parse()

	if fileExists(keysDir + "/auth-test-pub.json") {
		fmt.Printf("Keys already generated. To re-generate, delete files: %s/auth-test-pub.json, %s/auth-test.json, %s/auth-token*String\n", keysDir, keysDir, keysDir)
		return
	}
	//Generate RSA Keypair
	pub, priv, _ := keygen_tools.GenKeys(2048)

	//Generate keys in JWK format
	pubJSJWKS, privJSJWKS, kid, _ := keygen_tools.GenJSJWKS(priv, pub)

	tokenString, err := getTokenString(createTokenWithClaims("jdoe123@example.com"), kid, priv)
	if err != nil {
		fmt.Printf("Token Signing error: %v\n", err)
	}

	tokenString2, err := getTokenString(createTokenWithClaims("bob@example.com"), kid, priv)
	if err != nil {
		fmt.Printf("Token Signing error: %v\n", err)
	}

	tokenAdminString, err := getTokenString(createTokenWithClaims("admin@example.com"), kid, priv)
	if err != nil {
		fmt.Printf("Token Signing error: %v\n", err)
	}

	tokenUnallowedString, err := getTokenString(createTokenWithClaims("unallowed@example.com"), kid, priv)
	if err != nil {
		fmt.Printf("Token Signing error: %v\n", err)
	}

	fmt.Printf("Generating Keys and Token to path: %s\n", keysDir)
	err = newFile(keysDir+"/auth-test-pub.json", pubJSJWKS, 0400)
	if err != nil {
		fmt.Printf("Failed to write file auth-test-pub.json: %v\n", err)
	}
	err = newFile(keysDir+"/auth-test.json", privJSJWKS, 0400)
	if err != nil {
		fmt.Printf("Failed to write file auth-test.json: %v\n", err)
	}
	err = newFile(keysDir+"/auth-tokenString", []byte(tokenString), 0400)
	if err != nil {
		fmt.Printf("Failed to write file auth-tokenString: %v\n", err)
	}
	err = newFile(keysDir+"/auth-tokenString2", []byte(tokenString2), 0400)
	if err != nil {
		fmt.Printf("Failed to write file auth-tokenString2: %v\n", err)
	}
	err = newFile(keysDir+"/auth-tokenAdminString", []byte(tokenAdminString), 0400)
	if err != nil {
		fmt.Printf("Failed to write file auth-tokenAdminString: %v\n", err)
	}
	err = newFile(keysDir+"/auth-tokenUnallowedString", []byte(tokenUnallowedString), 0400)
	if err != nil {
		fmt.Printf("Failed to write file auth-tokenUnallowedString: %v\n", err)
	}
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func newFile(filename string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	n, err := f.Write(data)
	if err == nil && n < len(data) {
		err = io.ErrShortWrite
	}
	if err1 := f.Close(); err == nil {
		err = err1
	}
	return err
}

func getTokenString(token *jwt.Token, kid string, priv crypto.PrivateKey) (string, error) {
	token.Header["kid"] = kid
	return token.SignedString(priv)
}

func createTokenWithClaims(email string) *jwt.Token {
	return jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"account_number": "1234567",
		"is_internal":    false,
		"is_active":      true,
		"account_id":     "7654321",
		"org_id":         "1010101",
		"last_name":      "Doe",
		"type":           "User",
		"locale":         "en_US",
		"first_name":     "John",
		"email":          email,
		"username":       email,
		"is_org_admin":   false,
		"clientId":       "1234",
	})
}
