// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package saml

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	dsig "github.com/russellhaering/goxmldsig"
)

// loadKeyPair reads the optional SP certificate and key. Both or neither:
// a certificate without its key cannot sign, and a key without its
// certificate cannot be published for the identity provider to check the
// signature with. The key must belong to the certificate, be RSA of at
// least 2048 bits or ECDSA, and the certificate must parse; anything else
// stops the boot rather than surfacing as a signature the identity provider
// refuses at the first sign-in.
func loadKeyPair(certFile, keyFile string) (crypto.Signer, *x509.Certificate, string, error) {
	certFile, keyFile = strings.TrimSpace(certFile), strings.TrimSpace(keyFile)
	switch {
	case certFile == "" && keyFile == "":
		return nil, nil, "", nil
	case certFile == "" || keyFile == "":
		return nil, nil, "", errors.New("saml: sp_certificate_file and sp_key_file go together: set both, or neither")
	}

	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, nil, "", fmt.Errorf("saml: read sp_certificate_file: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, "", fmt.Errorf("saml: sp_certificate_file %s holds no PEM CERTIFICATE block", certFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, "", fmt.Errorf("saml: sp_certificate_file %s: %w", certFile, err)
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, nil, "", fmt.Errorf("saml: read sp_key_file: %w", err)
	}
	signer, err := parsePrivateKey(keyPEM)
	if err != nil {
		return nil, nil, "", fmt.Errorf("saml: sp_key_file %s: %w", keyFile, err)
	}

	var method string
	switch k := signer.(type) {
	case *rsa.PrivateKey:
		if k.N.BitLen() < 2048 {
			return nil, nil, "", fmt.Errorf("saml: sp_key_file %s is a %d-bit RSA key; 2048 bits is the minimum", keyFile, k.N.BitLen())
		}
		method = dsig.RSASHA256SignatureMethod
	case *ecdsa.PrivateKey:
		method = dsig.ECDSASHA256SignatureMethod
	default:
		return nil, nil, "", fmt.Errorf("saml: sp_key_file %s is a %T; an RSA or ECDSA key is required", keyFile, signer)
	}
	if !publicKeysEqual(signer.Public(), cert.PublicKey) {
		return nil, nil, "", fmt.Errorf("saml: sp_key_file %s is not the key of sp_certificate_file %s", keyFile, certFile)
	}
	return signer, cert, method, nil
}

func parsePrivateKey(raw []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("no PEM block")
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("a %T cannot sign", key)
		}
		return signer, nil
	}
	return nil, fmt.Errorf("a PEM block of type %q is not a private key (want PRIVATE KEY, RSA PRIVATE KEY or EC PRIVATE KEY)", block.Type)
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	type equaler interface{ Equal(crypto.PublicKey) bool }
	e, ok := a.(equaler)
	return ok && e.Equal(b)
}
