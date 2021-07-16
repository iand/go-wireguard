package wireguard

import (
	"fmt"
	"log"
	"net"
)

type UDPConn interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	Close() error
}

type packet struct {
	addr *net.UDPAddr
	data []byte
}

func (f *Interface) acceptOutsidePackets() {
	for {
		buf := make([]byte, mtu)
		log.Println("wip: f.outside.ReadFromUDP()")
		n, addr, err := f.outside.ReadFromUDP(buf)
		log.Printf("wip: f.outside.ReadFromUDP() finished: (%d, %+v, %s)", n, addr, err)

		//lint:ignore SA9003 figure out what kind of errors can be returned
		if err != nil {
			// TODO: figure out what kind of errors can be returned
		}

		// TODO: fire off a goroutine here
		buf = buf[:n]
		f.receiveOutsidePacket(packet{addr, buf})
	}
}

func (f *Interface) receiveOutsidePacket(p packet) {
	log.Printf("received packet: %x\n", p.data)
	switch checkMessageType(p.data) {
	case messageHandshakeInitiation:
		// queue handshake
	case messageHandshakeResponse:
		log.Println("received messageHandshakeResponse")
		f.receiveHandshakePacket(messageHandshakeResponse, p)
	case messageHandshakeCookie:
	case messageData:
		// queue for data processing
	default:
		if len(p.data) == 0 {
			log.Printf("wip: invalid packet: 0 byte packet")
		} else if len(p.data) > 0 && p.data[0] != byte(messageData) {
			log.Printf("wip: invalid packet: invalid type=%d", p.data[0])
		} else {
			log.Printf("wip: invalid packet: data packet of size %d is too small (must be >=%d)", len(p.data), messageDataMinLen)
		}
	}
}

func (f *Interface) receiveHandshakePacket(typ messageType, p packet) {
	// TODO: cookie check
	var peer *peer
	var err error

	switch typ {
	case messageHandshakeInitiation:
		peer, err = f.handshakeConsumeInitiation(p.data)
		if err != nil {
			// TODO: log error
			return
		}
		peer.updateLatestAddr(p.addr)
		res, err := f.handshakeCreateResponse(&peer.handshake)
		if err != nil {
			// TODO: log error
			return
		}
		_ = res
	case messageHandshakeResponse:
		peer, err = f.handshakeConsumeResponse(p.data)
		if err != nil {
			// TODO: log error
			return
		}
		peer.timerEphemeralKeyCreated()
		peer.timerHandshakeComplete()
	default:
		panic(fmt.Sprintf("invalid packet type %d from %s in receiveHandshakePacket", typ, p.addr))
	}

	peer.rxStats(len(p.data))
	peer.timerAnyAuthenticatedPacketReceived()
	peer.timerAnyAuthenticatedPacketTraversal()
	peer.updateLatestAddr(p.addr)
}
