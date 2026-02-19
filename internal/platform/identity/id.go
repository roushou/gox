package identity

import (
	"crypto/rand"
	"encoding/hex"
	"sync/atomic"
	"time"
)

var counter uint64

func NewID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	seq := atomic.AddUint64(&counter, 1)
	return hex.EncodeToString(buf[:]) + "-" + time.Now().UTC().Format("20060102150405.000000000") + "-" + uintToBase36(seq)
}

func uintToBase36(v uint64) string {
	if v == 0 {
		return "0"
	}
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	var out [16]byte
	i := len(out)
	for v > 0 {
		i--
		out[i] = alphabet[v%36]
		v /= 36
	}
	return string(out[i:])
}
