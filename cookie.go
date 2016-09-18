package wireguard

import (
	"encoding/binary"
	"hash"
	"log"
	"sync"
	"time"

	"github.com/flynn/go-wireguard/internal/blake2s"

	"github.com/devi/chap"
)

const (
	cookieSecretMaxAge  = 2 * time.Minute
	cookieSecretLatency = 2 * time.Second

	cookieSaltLen = 32
	cookieLen     = 16
)

type cookie struct {
	birthdate time.Time
	valid     bool
	cookie    [cookieLen]byte
	sentMAC1  bool
	lastMAC1  [cookieLen]byte
	sync.RWMutex
}

func (f *Interface) cookieAddMACs(msg []byte, peer *peer) []byte {
	if cap(msg) < len(msg)+(cookieLen*2) {
		panic("msg is not long enough")
	}
	f.identityMtx.RLock()
	defer f.identityMtx.RUnlock()

	// mac1
	var h hash.Hash
	if len(f.presharedKey) > 0 {
		h = blake2s.NewMAC(16, f.presharedKey)
	} else {
		h = blake2s.NewMAC(16, []byte{})
	}
	h.Write(peer.handshake.remoteStatic[:])
	log.Printf("len(peer.handshake.remoteStatic=%d)", len(peer.handshake.remoteStatic))
	h.Write(msg)
	log.Printf("before mac1: len(msg)=%d\n", len(msg))
	msg = h.Sum(msg)
	log.Printf("after mac1: len(msg)=%d\n", len(msg))

	peer.latestCookie.Lock()
	copy(peer.latestCookie.lastMAC1[:], msg[len(msg)-cookieLen:])
	peer.latestCookie.sentMAC1 = true
	peer.latestCookie.Unlock()

	// mac2
	peer.latestCookie.RLock()
	defer peer.latestCookie.RUnlock()
	if peer.latestCookie.valid && time.Now().Before(peer.latestCookie.birthdate.Add(cookieSecretMaxAge-cookieSecretLatency)) {
		h := blake2s.NewMAC(16, peer.latestCookie.cookie[:])
		h.Write(msg)
		log.Printf("before mac2: len(msg)=%d\n", len(msg))
		h.Sum(msg)
		log.Printf("after mac2: len(msg)=%d\n", len(msg))
	} else {
		// mac2 is all zeros if there is no valid cookie
		log.Printf("before mac2: len(msg)=%d\n", len(msg))
		msg = msg[:len(msg)+cookieLen]
		return msg
		log.Printf("after mac2: len(msg)=%d\n", len(msg))
	}

	return msg
}

var chapZeroNonce = make([]byte, chap.NonceSize)

func (f *Interface) cookieMessageConsume(msg []byte) {
	var peer *peer
	receiverIndex := binary.LittleEndian.Uint32(msg[1:])

	f.handshakesMtx.RLock()
	handshake, ok := f.handshakes[receiverIndex]
	f.handshakesMtx.RUnlock()
	if ok {
		peer = handshake.peer
	} else {
		f.keypairsMtx.RLock()
		keypair, ok := f.keypairs[receiverIndex]
		f.keypairsMtx.RUnlock()
		if ok {
			peer = keypair.peer
		}
	}
	if peer == nil {
		return
	}

	peer.latestCookie.RLock()
	if peer.latestCookie.sentMAC1 {
		return
	}
	peer.latestCookie.RUnlock()

	var h hash.Hash
	f.identityMtx.RLock()
	if len(f.staticKey.Private) == 0 {
		f.identityMtx.RUnlock()
		return
	}
	if len(f.presharedKey) > 0 {
		h = blake2s.NewMAC(16, f.presharedKey)
	} else {
		h = blake2s.NewMAC(16, []byte{})
	}
	f.identityMtx.RUnlock()

	var key [32]byte
	h.Write(peer.handshake.remoteStatic[:])
	h.Write(msg[5 : 5+cookieSaltLen])
	h.Sum(key[:0])

	peer.latestCookie.Lock()
	defer peer.latestCookie.Unlock()
	cipher := chap.NewCipher(&key)
	_, err := cipher.Open(peer.latestCookie.cookie[:0], chapZeroNonce, msg[5+cookieSaltLen:], peer.latestCookie.lastMAC1[:])
	if err != nil {
		// TODO: log error
		return
	}
	peer.latestCookie.birthdate = time.Now()
	peer.latestCookie.valid = true
	peer.latestCookie.sentMAC1 = false
}
