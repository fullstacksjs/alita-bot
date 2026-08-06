package modules

import (
	"crypto/rand"
	"encoding/binary"
)

func uniqueModuleChatID() int64 {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("uniqueModuleChatID: crypto/rand failed: " + err.Error())
	}
	n := int64(binary.BigEndian.Uint64(buf[:]) & 0x7fffffffffffffff)
	return -1000000000000 - n
}
