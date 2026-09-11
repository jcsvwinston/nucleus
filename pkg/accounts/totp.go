package accounts

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP is implemented here, against RFC 6238, rather than pulled in as a
// dependency. It is about sixty lines of HMAC and a counter, the algorithm
// has not changed since 2011, and the framework's rule is stdlib-first: a
// dependency that small is a supply-chain entry for every application that
// links the framework.
//
// SHA-1 is the hash, and that is not an oversight: RFC 6238 defines
// HMAC-SHA1 as the default, and it is what every authenticator app
// implements. The construction's security here rests on HMAC with a secret,
// not on collision resistance.

const (
	// totpDigits is the code length every authenticator app expects.
	totpDigits = 6
	// totpPeriod is the step, in seconds. 30 is the universal default.
	totpPeriod = 30
	// totpSkew is how many steps either side are accepted, for clocks
	// that disagree and for a code typed as it rolls over. One step each
	// way is the usual compromise: it widens the window to 90 seconds
	// rather than to minutes.
	totpSkew = 1
	// totpSecretBytes is the shared secret's size. 20 bytes is the RFC
	// 4226 recommendation and what apps expect from a base32 string.
	totpSecretBytes = 20
)

// NewTOTPSecret returns a fresh base32 secret, in the alphabet
// authenticator apps read (no padding, upper case).
func NewTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("accounts: generate TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// TOTPURI builds the otpauth:// URI an authenticator app scans as a QR
// code. issuer is the product name the app shows; account is what
// distinguishes one entry from another, usually the email address.
//
// The issuer appears TWICE — in the label and as a parameter — because
// that is what the de-facto spec requires and what makes an app group
// entries correctly. Getting it wrong is why an app sometimes shows
// "Unknown" next to a code.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(strings.TrimSpace(issuer) + ":" + strings.TrimSpace(account))
	params := url.Values{}
	params.Set("secret", strings.TrimSpace(secret))
	params.Set("issuer", strings.TrimSpace(issuer))
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprint(totpDigits))
	params.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + params.Encode()
}

// totpCode computes the code for one counter step.
func totpCode(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("accounts: decode TOTP secret: %w", err)
	}

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// Dynamic truncation, RFC 4226 §5.3.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, value%pow10(totpDigits)), nil
}

// VerifyTOTP reports whether code is valid for the secret at this time,
// and returns the COUNTER STEP it matched.
//
// The step is returned, and not discarded, for the property that makes a
// one-time password one-time: a caller records the last accepted step and
// refuses anything at or below it. Without that, a code shouted across a
// room works for the rest of its thirty seconds, and every replay inside
// the window succeeds.
//
// The comparison is constant-time. A code is a six-digit secret; comparing
// it with == leaks how many leading digits were right to anyone who can
// measure, which is what turns a million guesses into a thousand.
func VerifyTOTP(secret, code string, at time.Time) (uint64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	current := uint64(at.UTC().Unix()) / totpPeriod
	for delta := -totpSkew; delta <= totpSkew; delta++ {
		counter := current
		switch {
		case delta < 0:
			step := uint64(-delta)
			if counter < step {
				continue
			}
			counter -= step
		case delta > 0:
			counter += uint64(delta)
		}
		candidate, err := totpCode(secret, counter)
		if err != nil {
			return 0, false
		}
		if hmac.Equal([]byte(candidate), []byte(code)) {
			return counter, true
		}
	}
	return 0, false
}

func pow10(n int) uint32 {
	out := uint32(1)
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}
