// Copyright 2026 jcsvwinston/nucleus
// SPDX-License-Identifier: Apache-2.0

package saml_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	crewjam "github.com/crewjam/saml"

	"github.com/jcsvwinston/nucleus/pkg/auth/backend"
	"github.com/jcsvwinston/nucleus/pkg/auth/federated"

	saml "github.com/jcsvwinston/nucleus/providers/auth-saml"
	"github.com/jcsvwinston/nucleus/providers/auth-saml/samltest"
)

func build(t *testing.T, cfg map[string]any) (federated.Provider, error) {
	t.Helper()
	factory, ok := federated.Lookup(saml.ProviderName)
	if !ok {
		t.Fatal("the blank import did not register saml")
	}
	return factory(backend.Config{Name: "corp", ProviderConfig: cfg})
}

func writeFile(t *testing.T, name string, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// keyPair writes a PEM certificate and key, RSA of the given size or ECDSA
// P-256 when bits is 0.
func keyPair(t *testing.T, bits int) (certFile, keyFile string, cert *x509.Certificate) {
	t.Helper()
	var signer crypto.Signer
	var keyDER []byte
	var err error
	if bits == 0 {
		var k *ecdsa.PrivateKey
		k, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		signer = k
	} else {
		var k *rsa.PrivateKey
		k, err = rsa.GenerateKey(rand.Reader, bits)
		signer = k
	}
	if err != nil {
		t.Fatal(err)
	}
	if keyDER, err = x509.MarshalPKCS8PrivateKey(signer); err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "sp"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, signer.Public(), signer)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ = x509.ParseCertificate(der)
	certFile = writeFile(t, "sp.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyFile = writeFile(t, "sp.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	return certFile, keyFile, cert
}

// The configuration is strict: a key this provider does not read, a
// missing or doubled source of metadata, a key pair that does not belong
// together, and a metadata URL over plain http stop the boot.
func TestConfig_RefusesWhatItCannotHonour(t *testing.T) {
	idp, err := samltest.New()
	if err != nil {
		t.Fatal(err)
	}
	idp.EntityID = "https://idp.example.test/metadata"
	idp.Start()
	t.Cleanup(idp.Close)
	md, err := idp.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	mdFile := writeFile(t, "idp.xml", md)
	certA, keyA, _ := keyPair(t, 2048)
	_, keyB, _ := keyPair(t, 2048)
	smallCert, smallKey, _ := keyPair(t, 1024)

	base := func(extra map[string]any) map[string]any {
		cfg := map[string]any{"sp_entity_id": entityID, "idp_metadata_file": mdFile}
		for k, v := range extra {
			if v == nil {
				delete(cfg, k)
				continue
			}
			cfg[k] = v
		}
		return cfg
	}
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"an unknown key", base(map[string]any{"allow_idp_initiated": true}), "allow_idp_initiated"},
		{"no sp_entity_id", base(map[string]any{"sp_entity_id": nil}), "sp_entity_id is required"},
		{"no metadata", base(map[string]any{"idp_metadata_file": nil}), "one of idp_metadata_url and idp_metadata_file"},
		{"two sources of metadata", base(map[string]any{"idp_metadata_url": "https://idp.example.test/metadata"}), "both set"},
		{"metadata over plain http", map[string]any{"sp_entity_id": entityID, "idp_metadata_url": "http://idp.example.test/metadata"}, "plain http"},
		{"a certificate without its key", base(map[string]any{"sp_certificate_file": certA}), "go together"},
		{"a key that is not the certificate's", base(map[string]any{"sp_certificate_file": certA, "sp_key_file": keyB}), "is not the key of"},
		{"a 1024-bit RSA key", base(map[string]any{"sp_certificate_file": smallCert, "sp_key_file": smallKey}), "2048 bits"},
		{"an unknown NameID format", base(map[string]any{"name_id_format": "whatever"}), "name_id_format"},
		{"a metadata file that does not exist", base(map[string]any{"idp_metadata_file": filepath.Join(t.TempDir(), "missing.xml")}), "idp_metadata_file"},
		{"a refresh under a minute", map[string]any{"sp_entity_id": entityID, "idp_metadata_url": "https://idp.example.test/metadata", "metadata_refresh": "10s"}, "metadata_refresh"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := build(t, c.cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("built with %v: err=%v, want one naming %q", c.cfg, err, c.want)
			}
		})
	}

	if _, err := build(t, base(map[string]any{"sp_certificate_file": certA, "sp_key_file": keyA, "name_id_format": "persistent"})); err != nil {
		t.Fatalf("a correct configuration was refused: %v", err)
	}
	ecCert, ecKey, _ := keyPair(t, 0)
	if _, err := build(t, base(map[string]any{"sp_certificate_file": ecCert, "sp_key_file": ecKey})); err != nil {
		t.Fatalf("an ECDSA key pair was refused: %v", err)
	}
}

// A metadata URL is read on the first sign-in, not at boot: the
// application starts when the identity provider is down, and the sign-in
// says the provider is unavailable — not that the person was rejected.
func TestMetadataURL_IsLazyAndUnavailableIsNotARejection(t *testing.T) {
	idp, err := samltest.New()
	if err != nil {
		t.Fatal(err)
	}
	idp.Start()
	url := idp.MetadataURL()
	idp.Close()

	p, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_url": url, "timeout": "2s"})
	if err != nil {
		t.Fatalf("an unreachable metadata URL stopped the boot: %v", err)
	}
	_, err = p.Begin(context.Background(), federated.BeginRequest{CallbackURL: callback, Nonce: "bm9uY2Utbm9uY2Utbm9uY2Utbm9uY2Utbm9uY2U"})
	if !errors.Is(err, federated.ErrProviderUnavailable) {
		t.Fatalf("Begin against an unreachable identity provider: %v, want ErrProviderUnavailable", err)
	}
}

func TestMetadataFile_SelectsAndChecksTheIdentityProvider(t *testing.T) {
	idp, err := samltest.New()
	if err != nil {
		t.Fatal(err)
	}
	idp.EntityID = "https://idp.example.test/metadata"
	idp.Start()
	t.Cleanup(idp.Close)
	md, err := idp.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	var one crewjam.EntityDescriptor
	if err := xml.Unmarshal(md, &one); err != nil {
		t.Fatal(err)
	}
	two := one
	two.EntityID = "https://other-idp.example.test/metadata"
	aggregate, err := xml.Marshal(crewjam.EntitiesDescriptor{EntityDescriptors: []crewjam.EntityDescriptor{one, two}})
	if err != nil {
		t.Fatal(err)
	}
	aggFile := writeFile(t, "aggregate.xml", aggregate)

	if _, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_file": aggFile}); err == nil || !strings.Contains(err.Error(), "idp_entity_id") {
		t.Fatalf("an aggregate without idp_entity_id: %v, want the error to ask for it", err)
	}
	if _, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_file": aggFile, "idp_entity_id": two.EntityID}); err != nil {
		t.Fatalf("an aggregate with idp_entity_id: %v", err)
	}
	if _, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_file": aggFile, "idp_entity_id": "https://nobody.example.test"}); err == nil {
		t.Fatal("an idp_entity_id the aggregate does not hold was accepted")
	}

	mutate := func(edit func(d *crewjam.EntityDescriptor)) string {
		d := one
		d.IDPSSODescriptors = append([]crewjam.IDPSSODescriptor(nil), one.IDPSSODescriptors...)
		edit(&d)
		raw, err := xml.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		return writeFile(t, "idp.xml", raw)
	}
	for name, c := range map[string]struct {
		edit func(d *crewjam.EntityDescriptor)
		want string
	}{
		"expired metadata": {func(d *crewjam.EntityDescriptor) { d.ValidUntil = time.Now().Add(-time.Hour) }, "expired"},
		"no redirect binding": {func(d *crewjam.EntityDescriptor) {
			d.IDPSSODescriptors[0].SingleSignOnServices = []crewjam.Endpoint{{Binding: crewjam.HTTPPostBinding, Location: "https://idp.example.test/sso"}}
		}, "HTTP-Redirect"},
		"no signing certificate": {func(d *crewjam.EntityDescriptor) {
			d.IDPSSODescriptors[0].KeyDescriptors = []crewjam.KeyDescriptor{{Use: "encryption", KeyInfo: d.IDPSSODescriptors[0].KeyDescriptors[0].KeyInfo}}
		}, "signing certificate"},
		"no identity provider": {func(d *crewjam.EntityDescriptor) { d.IDPSSODescriptors = nil }, "no identity provider"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_file": mutate(c.edit)})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err=%v, want one naming %q", err, c.want)
			}
		})
	}
}

// The service-provider metadata the framework serves advertises what the
// provider does: one HTTP-POST AssertionConsumerService at the callback,
// assertions wanted signed, no encryption key — and, with a key pair, the
// signing certificate and AuthnRequestsSigned.
func TestServiceMetadata(t *testing.T) {
	h := newHarness(t, nil)
	_ = h
	plain, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_url": h.idp.MetadataURL()})
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile, cert := keyPair(t, 2048)
	signing, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_url": h.idp.MetadataURL(),
		"sp_certificate_file": certFile, "sp_key_file": keyFile, "name_id_format": "persistent"})
	if err != nil {
		t.Fatal(err)
	}

	type publisher interface {
		ServiceMetadata(ctx context.Context, callbackURL string) (string, []byte, error)
	}
	read := func(p federated.Provider) (string, crewjam.EntityDescriptor) {
		pub, ok := p.(publisher)
		if !ok {
			t.Fatal("the provider publishes no service-provider metadata")
		}
		ctype, body, err := pub.ServiceMetadata(context.Background(), callback)
		if err != nil {
			t.Fatal(err)
		}
		var md crewjam.EntityDescriptor
		if err := xml.Unmarshal(body, &md); err != nil {
			t.Fatalf("the metadata does not parse: %v\n%s", err, body)
		}
		return ctype, md
	}

	ctype, md := read(plain)
	if ctype != "application/samlmetadata+xml" {
		t.Errorf("content type %q", ctype)
	}
	if md.EntityID != entityID || len(md.SPSSODescriptors) != 1 {
		t.Fatalf("metadata %+v", md)
	}
	sp := md.SPSSODescriptors[0]
	if len(sp.AssertionConsumerServices) != 1 || sp.AssertionConsumerServices[0].Binding != crewjam.HTTPPostBinding || sp.AssertionConsumerServices[0].Location != callback {
		t.Errorf("AssertionConsumerServices %+v, want one HTTP-POST at %s", sp.AssertionConsumerServices, callback)
	}
	if sp.WantAssertionsSigned == nil || !*sp.WantAssertionsSigned {
		t.Error("WantAssertionsSigned is not true")
	}
	if len(sp.KeyDescriptors) != 0 {
		t.Errorf("without a key pair the metadata publishes keys: %+v", sp.KeyDescriptors)
	}
	if sp.AuthnRequestsSigned != nil && *sp.AuthnRequestsSigned {
		t.Error("without a key pair AuthnRequestsSigned is true")
	}

	_, md = read(signing)
	sp = md.SPSSODescriptors[0]
	if sp.AuthnRequestsSigned == nil || !*sp.AuthnRequestsSigned {
		t.Error("with a key pair AuthnRequestsSigned is not true")
	}
	if len(sp.KeyDescriptors) != 1 || sp.KeyDescriptors[0].Use != "signing" ||
		sp.KeyDescriptors[0].KeyInfo.X509Data.X509Certificates[0].Data != base64.StdEncoding.EncodeToString(cert.Raw) {
		t.Errorf("with a key pair the metadata publishes %+v, want the signing certificate and no encryption key", sp.KeyDescriptors)
	}
	if len(sp.NameIDFormats) != 1 || sp.NameIDFormats[0] != crewjam.PersistentNameIDFormat {
		t.Errorf("NameIDFormats %v, want persistent", sp.NameIDFormats)
	}
}

// With a key pair the AuthnRequest is signed with the HTTP-Redirect
// binding's query signature, which verifies against the published
// certificate.
func TestSignIn_AuthnRequestSignedWithAKeyPair(t *testing.T) {
	certFile, keyFile, cert := keyPair(t, 2048)
	h := newHarness(t, map[string]any{"sp_certificate_file": certFile, "sp_key_file": keyFile})
	req, token := h.begin()
	if !req.Signed || req.SigAlg != "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256" {
		t.Fatalf("the AuthnRequest is not signed with RSA-SHA256: %+v", req)
	}
	sum := sha256.Sum256([]byte(req.SignedQuery))
	if err := rsa.VerifyPKCS1v15(cert.PublicKey.(*rsa.PublicKey), crypto.SHA256, sum[:], req.Signature); err != nil {
		t.Fatalf("the AuthnRequest's signature does not verify with the SP certificate: %v", err)
	}
	form, err := h.idp.Respond(req, samltest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.post(token, form.Values()); err != nil {
		t.Fatalf("a signed sign-in was refused: %v", err)
	}
}

// The identity: NameID as ID, the configured attributes, roles only when a
// role attribute is named, and the NameID as email when its format says it
// is one.
func TestSignIn_MapsTheIdentity(t *testing.T) {
	h := newHarness(t, map[string]any{"username_attribute": "login", "email_attribute": "work_email", "role_attribute": "groups"})
	h.idp.User = samltest.User{
		NameID: "00u1abcd",
		Attributes: map[string][]string{
			"login":      {"ana"},
			"work_email": {"ana@corp.example"},
			"mail":       {"ignored@example.test"},
			"groups":     {"admins", "staff"},
		},
	}
	form, token := h.respond(samltest.Options{})
	user, err := h.post(token, form.Values())
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "00u1abcd" || user.Username != "ana" || user.Email != "ana@corp.example" ||
		user.Role != "admins" || strings.Join(user.Roles, ",") != "admins,staff" {
		t.Fatalf("identity %+v", user)
	}

	h = newHarness(t, nil)
	h.idp.User = samltest.User{NameID: "bo@corp.example", NameIDFormat: string(crewjam.EmailAddressNameIDFormat)}
	form, token = h.respond(samltest.Options{})
	user, err = h.post(token, form.Values())
	if err != nil {
		t.Fatal(err)
	}
	if user.Email != "bo@corp.example" || user.Username != "bo@corp.example" || len(user.Roles) != 0 {
		t.Fatalf("identity from an emailAddress NameID %+v", user)
	}
}

// The provider tells the framework its callback is a cross-site form post,
// so the state cookie can ride it.
func TestProvider_DeclaresACrossSiteFormPostCallback(t *testing.T) {
	h := newHarness(t, nil)
	p, err := build(t, map[string]any{"sp_entity_id": entityID, "idp_metadata_url": h.idp.MetadataURL()})
	if err != nil {
		t.Fatal(err)
	}
	cs, ok := p.(interface{ CallbackIsCrossSiteFormPost() bool })
	if !ok || !cs.CallbackIsCrossSiteFormPost() {
		t.Fatal("the provider does not declare its callback a cross-site form post")
	}
}
