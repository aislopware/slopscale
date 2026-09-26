package capture

import "encoding/binary"

// splitUDP hands each datagram of a UDP segmentation-offloaded packet (one
// IP and UDP header for several datagrams of segSize bytes, the last one
// shorter) to emit as a packet of its own, built in scratch. Anything else
// is handed over as it is.
func splitUDP(pkt []byte, segSize int, scratch []byte, emit func([]byte)) {
	proto, hdrLen, ok := ipHeader(pkt)
	if !ok || proto != protoUDP || segSize <= 0 || len(pkt) < hdrLen+udpHeader {
		emit(pkt)

		return
	}

	headers := hdrLen + udpHeader

	for data := pkt[headers:]; len(data) > 0; {
		n := min(segSize, len(data))

		seg := append(append(scratch[:0], pkt[:headers]...), data[:n]...)
		setLengths(seg, hdrLen)
		emit(seg)

		data = data[n:]
	}
}

// setLengths rewrites the IP and UDP length fields of a datagram cut from
// an offloaded packet.
func setLengths(seg []byte, hdrLen int) {
	//nolint:gosec // one datagram of a packet under 64 KiB
	if seg[0]>>ipVersionShift == ipVersion4 {
		binary.BigEndian.PutUint16(seg[2:4], uint16(len(seg)))
	} else {
		binary.BigEndian.PutUint16(seg[4:6], uint16(len(seg)-ipv6Header))
	}

	//nolint:gosec // as above
	binary.BigEndian.PutUint16(seg[hdrLen+4:hdrLen+6], uint16(len(seg)-hdrLen))
}
