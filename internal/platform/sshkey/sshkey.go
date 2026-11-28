package sshkey

import (
	"bytes"
	"crypto/ecdh"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strings"
)

const (
	keyBits         = 2048
	publicExponent  = 65537
	generatedSuffix = "Generated-by-Nova"
)

var ErrInvalidPublicKey = errors.New("invalid OpenSSH public key")

var ecdsaCurves = map[string]struct {
	name  string
	curve elliptic.Curve
	ecdh  ecdh.Curve
}{
	"ecdsa-sha2-nistp256": {"nistp256", elliptic.P256(), ecdh.P256()},
	"ecdsa-sha2-nistp384": {"nistp384", elliptic.P384(), ecdh.P384()},
	"ecdsa-sha2-nistp521": {"nistp521", elliptic.P521(), ecdh.P521()},
}

func Fingerprint(line string) (string, error) {
	fields := strings.SplitN(line, " ", 3)
	if len(fields) < 2 {
		return "", ErrInvalidPublicKey
	}
	blob, err := base64.StdEncoding.DecodeString(strings.TrimRight(fields[1], "\r\n"))
	if err != nil {
		return "", ErrInvalidPublicKey
	}
	if err := validateBlob(fields[0], blob); err != nil {
		return "", err
	}
	digest := md5.Sum(blob)
	encoded := hex.EncodeToString(digest[:])
	pairs := make([]string, 0, len(digest))
	for i := 0; i < len(encoded); i += 2 {
		pairs = append(pairs, encoded[i:i+2])
	}
	return strings.Join(pairs, ":"), nil
}

func validateBlob(keyType string, blob []byte) error {
	reader := wireReader{data: blob}
	if inner, ok := reader.bytes(); !ok || string(inner) != keyType {
		return ErrInvalidPublicKey
	}
	switch keyType {
	case "ssh-rsa":
		e, okE := reader.mpint()
		n, okN := reader.mpint()
		three := big.NewInt(3)
		if !okE || !okN || n.Cmp(three) < 0 || e.Cmp(three) < 0 || e.Cmp(n) >= 0 || e.Bit(0) == 0 {
			return ErrInvalidPublicKey
		}
	case "ssh-dss":
		p, okP := reader.mpint()
		q, okQ := reader.mpint()
		g, okG := reader.mpint()
		_, okY := reader.mpint()
		if !okP || !okQ || !okG || !okY || !slices.Contains([]int{1024, 2048, 3072, 4096}, p.BitLen()) ||
			!slices.Contains([]int{160, 224, 256}, q.BitLen()) || g.Cmp(big.NewInt(1)) <= 0 || g.Cmp(p) >= 0 {
			return ErrInvalidPublicKey
		}
	case "ssh-ed25519":
		if key, ok := reader.bytes(); !ok || len(key) != 32 {
			return ErrInvalidPublicKey
		}
	default:
		curve, found := ecdsaCurves[keyType]
		if !found {
			return ErrInvalidPublicKey
		}
		name, okName := reader.bytes()
		point, okPoint := reader.bytes()
		if !okName || !okPoint || string(name) != curve.name || !validPoint(curve.curve, curve.ecdh, point) {
			return ErrInvalidPublicKey
		}
	}
	if len(reader.data) != 0 {
		return ErrInvalidPublicKey
	}
	return nil
}

func validPoint(curve elliptic.Curve, exchange ecdh.Curve, point []byte) bool {
	if len(point) > 0 && (point[0] == 2 || point[0] == 3) {
		x, _ := elliptic.UnmarshalCompressed(curve, point)
		return x != nil
	}
	_, err := exchange.NewPublicKey(point)
	return err == nil
}

type wireReader struct {
	data []byte
}

func (r *wireReader) bytes() ([]byte, bool) {
	if len(r.data) < 4 {
		return nil, false
	}
	size := binary.BigEndian.Uint32(r.data)
	if uint64(size) > uint64(len(r.data)-4) {
		return nil, false
	}
	value := r.data[4 : 4+size]
	r.data = r.data[4+size:]
	return value, true
}

func (r *wireReader) mpint() (*big.Int, bool) {
	value, ok := r.bytes()
	if !ok || (len(value) > 0 && value[0] > 0x7f) {
		return nil, false
	}
	return new(big.Int).SetBytes(value), true
}

func Generate(random io.Reader) (privateKey, publicKey string, err error) {
	e := big.NewInt(publicExponent)
	one := big.NewInt(1)
	for {
		p, err := prime(random, keyBits/2)
		if err != nil {
			return "", "", err
		}
		q, err := prime(random, keyBits/2)
		if err != nil {
			return "", "", err
		}
		if p.Cmp(q) == 0 {
			continue
		}
		pMinus, qMinus := new(big.Int).Sub(p, one), new(big.Int).Sub(q, one)
		gcd := new(big.Int).GCD(nil, nil, pMinus, qMinus)
		lcm := new(big.Int).Div(new(big.Int).Mul(pMinus, qMinus), gcd)
		d := new(big.Int).ModInverse(e, lcm)
		if d == nil {
			continue
		}
		key := &rsa.PrivateKey{
			PublicKey: rsa.PublicKey{N: new(big.Int).Mul(p, q), E: publicExponent},
			D:         d,
			Primes:    []*big.Int{p, q},
		}
		if key.N.BitLen() != keyBits {
			continue
		}
		key.Precompute()
		if err := key.Validate(); err != nil {
			return "", "", fmt.Errorf("generated RSA key is invalid: %w", err)
		}
		block := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		return string(block), authorizedKey(key.PublicKey), nil
	}
}

func prime(random io.Reader, bits int) (*big.Int, error) {
	candidate := make([]byte, bits/8)
	two := big.NewInt(2)
	for {
		if _, err := io.ReadFull(random, candidate); err != nil {
			return nil, err
		}
		candidate[0] |= 0xc0
		candidate[len(candidate)-1] |= 1
		p := new(big.Int).SetBytes(candidate)
		for p.BitLen() == bits {
			if p.ProbablyPrime(20) {
				return p, nil
			}
			p.Add(p, two)
		}
	}
}

func authorizedKey(key rsa.PublicKey) string {
	var blob bytes.Buffer
	writeString(&blob, []byte("ssh-rsa"))
	writeString(&blob, mpint(big.NewInt(int64(key.E))))
	writeString(&blob, mpint(key.N))
	return "ssh-rsa " + base64.StdEncoding.EncodeToString(blob.Bytes()) + " " + generatedSuffix
}

func mpint(value *big.Int) []byte {
	encoded := value.Bytes()
	if len(encoded) > 0 && encoded[0]&0x80 != 0 {
		encoded = append([]byte{0}, encoded...)
	}
	return encoded
}

func writeString(buffer *bytes.Buffer, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	buffer.Write(size[:])
	buffer.Write(value)
}
