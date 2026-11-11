package crypto

import (
	"bytes"
	"testing"
)

func newCipher(t *testing.T, fill byte) *Cipher {
	t.Helper()
	c, err := New(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newCipher(t, 1)
	sealed, err := c.Encrypt([]byte("sk-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("sk-secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := c.Decrypt(sealed)
	if err != nil || string(got) != "sk-secret" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNonceIsUniquePerEncryption(t *testing.T) {
	c := newCipher(t, 1)
	a, _ := c.Encrypt([]byte("same"))
	b, _ := c.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext are identical")
	}
}

func TestDecryptRejectsBadInput(t *testing.T) {
	c := newCipher(t, 1)
	sealed, _ := c.Encrypt([]byte("sk-secret"))
	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-1] ^= 1

	cases := map[string][]byte{
		"tampered":  tampered,
		"short":     sealed[:5],
		"empty":     nil,
		"wrong key": mustEncryptWith(t, newCipher(t, 2), "sk-secret"),
	}
	for name, input := range cases {
		if _, err := c.Decrypt(input); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func mustEncryptWith(t *testing.T, c *Cipher, s string) []byte {
	t.Helper()
	out, err := c.Encrypt([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestNewRejectsWrongKeyLength(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := New(make([]byte, n)); err == nil {
			t.Errorf("len %d: want error", n)
		}
	}
}
