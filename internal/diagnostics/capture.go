package diagnostics

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// Packet-capture input for diagnostics. A capture shows what was actually on
// the wire, which the DHCP server log cannot: the server logs its own
// decisions, while a capture also shows client REQUEST, DECLINE and RELEASE
// messages and offers sent by other servers. Only classic libpcap files with
// Ethernet framing are supported (tcpdump -w, tshark -F pcap). Packets are
// parsed with bounds checks and never trusted beyond the header fields used.

const (
	maxCaptureBytes   = 64 << 20
	maxCapturePackets = 200000
	linkTypeEthernet  = 1
	// A transfer still open when the capture ends is only reported as failed
	// if the capture kept running this long after its last packet; otherwise
	// the capture simply stopped too early to tell.
	tftpIdleTimeout = 10 * time.Second
)

var dhcpMessageNames = map[byte]string{
	1: "discover", 2: "offer", 3: "request", 4: "decline", 5: "ack", 6: "nak", 7: "release", 8: "inform",
}

// CaptureSource selects one client in a capture. ClientMAC is required when
// the capture contains more than one DHCP client; DeviceID is attached to the
// events so they correlate with -device-id filters.
type CaptureSource struct {
	Path      string
	ClientMAC string
	DeviceID  string
}

// ReadCapture converts DHCP and TFTP packets of one client into events named
// like the InfraFlow service events (dhcp.discover, dhcp.offer, dhcp.request,
// dhcp.ack, dhcp.nak, dhcp.decline, dhcp.release, tftp.rrq,
// tftp.transfer.completed, tftp.file_missing, tftp.transfer.failed), with
// Source "capture".
func ReadCapture(source CaptureSource) ([]Event, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, fmt.Errorf("open capture: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect capture: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxCaptureBytes {
		return nil, fmt.Errorf("capture must be a regular file no larger than %d bytes", maxCaptureBytes)
	}
	var client net.HardwareAddr
	if strings.TrimSpace(source.ClientMAC) != "" {
		client, err = net.ParseMAC(strings.TrimSpace(source.ClientMAC))
		if err != nil || len(client) != 6 {
			return nil, fmt.Errorf("invalid client MAC %q", source.ClientMAC)
		}
	}
	packets, err := readPcap(file)
	if err != nil {
		return nil, err
	}
	return captureEvents(packets, client, source.DeviceID)
}

type capturedPacket struct {
	at   time.Time
	data []byte
}

func readPcap(input io.Reader) ([]capturedPacket, error) {
	header := make([]byte, 24)
	if _, err := io.ReadFull(input, header); err != nil {
		return nil, fmt.Errorf("read capture header: %w", err)
	}
	var order binary.ByteOrder
	nanos := false
	switch magic := binary.LittleEndian.Uint32(header); magic {
	case 0xa1b2c3d4:
		order = binary.LittleEndian
	case 0xa1b23c4d:
		order, nanos = binary.LittleEndian, true
	case 0xd4c3b2a1:
		order = binary.BigEndian
	case 0x4d3cb2a1:
		order, nanos = binary.BigEndian, true
	case 0x0a0d0d0a:
		return nil, errors.New("pcapng captures are not supported; convert with: editcap -F pcap in.pcapng out.pcap")
	default:
		return nil, errors.New("not a libpcap capture file")
	}
	if link := order.Uint32(header[20:]); link != linkTypeEthernet {
		return nil, fmt.Errorf("unsupported capture link type %d (only Ethernet is supported)", link)
	}
	snaplen := order.Uint32(header[16:])
	if snaplen == 0 || snaplen > 262144 {
		snaplen = 262144
	}
	var packets []capturedPacket
	record := make([]byte, 16)
	for {
		if _, err := io.ReadFull(input, record); err != nil {
			if errors.Is(err, io.EOF) {
				return packets, nil
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return packets, nil // truncated final record, as written by an interrupted tcpdump
			}
			return nil, fmt.Errorf("read capture record: %w", err)
		}
		if len(packets) >= maxCapturePackets {
			return nil, fmt.Errorf("capture has more than %d packets; filter it first (udp port 67 or udp port 68 or udp port 69)", maxCapturePackets)
		}
		seconds, fraction := order.Uint32(record), order.Uint32(record[4:])
		length := order.Uint32(record[8:])
		if length > snaplen {
			return nil, fmt.Errorf("capture record of %d bytes exceeds the snapshot length", length)
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(input, data); err != nil {
			return packets, nil
		}
		nsec := int64(fraction) * 1000
		if nanos {
			nsec = int64(fraction)
		}
		packets = append(packets, capturedPacket{at: time.Unix(int64(seconds), nsec).UTC(), data: data})
	}
}

type udpPacket struct {
	at               time.Time
	srcMAC           net.HardwareAddr
	srcIP, dstIP     net.IP
	srcPort, dstPort uint16
	payload          []byte
}

func parseUDP(packet capturedPacket) (udpPacket, bool) {
	frame := packet.data
	if len(frame) < 14 {
		return udpPacket{}, false
	}
	etherType := binary.BigEndian.Uint16(frame[12:])
	offset := 14
	if etherType == 0x8100 && len(frame) >= 18 {
		etherType = binary.BigEndian.Uint16(frame[16:])
		offset = 18
	}
	if etherType != 0x0800 || len(frame) < offset+20 {
		return udpPacket{}, false
	}
	ip := frame[offset:]
	headerLength := int(ip[0]&0x0f) * 4
	if ip[0]>>4 != 4 || headerLength < 20 || ip[9] != 17 || len(ip) < headerLength+8 {
		return udpPacket{}, false
	}
	if fragment := binary.BigEndian.Uint16(ip[6:]); fragment&0x1fff != 0 || fragment&0x2000 != 0 {
		return udpPacket{}, false // fragments are not reassembled
	}
	udp := ip[headerLength:]
	udpLength := int(binary.BigEndian.Uint16(udp[4:]))
	if udpLength < 8 || udpLength > len(udp) {
		udpLength = len(udp)
	}
	return udpPacket{
		at: packet.at, srcMAC: net.HardwareAddr(frame[6:12]),
		srcIP: net.IP(ip[12:16]), dstIP: net.IP(ip[16:20]),
		srcPort: binary.BigEndian.Uint16(udp), dstPort: binary.BigEndian.Uint16(udp[2:]),
		payload: udp[8:udpLength],
	}, true
}

type dhcpPacket struct {
	messageType byte
	chaddr      net.HardwareAddr
	yiaddr      net.IP
	serverID    net.IP
	requested   net.IP
	bootfile    string
}

func parseDHCP(payload []byte) (dhcpPacket, bool) {
	if len(payload) < 240 || !bytes.Equal(payload[236:240], []byte{99, 130, 83, 99}) || payload[2] != 6 {
		return dhcpPacket{}, false
	}
	result := dhcpPacket{chaddr: net.HardwareAddr(payload[28:34]), yiaddr: net.IP(payload[16:20])}
	if file := strings.TrimRight(string(payload[108:236]), "\x00"); file != "" {
		result.bootfile = file
	}
	options := payload[240:]
	for index := 0; index < len(options); {
		code := options[index]
		if code == 255 {
			break
		}
		if code == 0 {
			index++
			continue
		}
		if index+1 >= len(options) {
			break
		}
		length := int(options[index+1])
		start, end := index+2, index+2+length
		if end > len(options) {
			break
		}
		value := options[start:end]
		switch code {
		case 53:
			if length == 1 {
				result.messageType = value[0]
			}
		case 54:
			if length == 4 {
				result.serverID = net.IP(value)
			}
		case 50:
			if length == 4 {
				result.requested = net.IP(value)
			}
		case 67:
			result.bootfile = string(value)
		}
		index = end
	}
	return result, result.messageType != 0
}

type tftpTransfer struct {
	file      string
	clientIP  string
	lastBlock uint16
	finalData bool
	done      bool
	started   time.Time
	lastSeen  time.Time
}

func captureEvents(packets []capturedPacket, client net.HardwareAddr, deviceID string) ([]Event, error) {
	clients := map[string]bool{}
	clientIPs := map[string]bool{}
	var events []Event
	transfers := map[string]*tftpTransfer{} // key: client ip:port
	emit := func(at time.Time, name, message, status string) {
		events = append(events, Event{
			Timestamp: at.Format(time.RFC3339Nano), Name: name, Source: "capture", Service: "capture",
			DeviceID: deviceID, Status: status, Message: message, Raw: message,
		})
	}
	var captureEnd time.Time
	for _, raw := range packets {
		if raw.at.After(captureEnd) {
			captureEnd = raw.at
		}
		packet, ok := parseUDP(raw)
		if !ok {
			continue
		}
		if (packet.dstPort == 67 || packet.dstPort == 68) && (packet.srcPort == 67 || packet.srcPort == 68) {
			dhcp, ok := parseDHCP(packet.payload)
			if !ok {
				continue
			}
			clients[dhcp.chaddr.String()] = true
			if client != nil && !bytes.Equal(dhcp.chaddr, client) {
				continue
			}
			name := dhcpMessageNames[dhcp.messageType]
			if name == "" {
				continue
			}
			message := fmt.Sprintf("DHCP %s on the wire: client %s", strings.ToUpper(name), dhcp.chaddr)
			if dhcp.serverID != nil {
				message += ", server " + dhcp.serverID.String()
			}
			if dhcp.requested != nil {
				message += ", requested " + dhcp.requested.String()
			}
			if !dhcp.yiaddr.IsUnspecified() {
				message += ", address " + dhcp.yiaddr.String()
			}
			if dhcp.bootfile != "" {
				message += ", bootfile " + dhcp.bootfile
			}
			if dhcp.messageType == 2 || dhcp.messageType == 5 {
				message += fmt.Sprintf(" (sent by %s)", packet.srcIP)
			}
			if dhcp.messageType == 5 && !dhcp.yiaddr.IsUnspecified() {
				clientIPs[dhcp.yiaddr.String()] = true
			}
			if dhcp.messageType == 8 {
				continue
			}
			status := map[byte]string{2: "OFFERED", 5: "COMPLETED", 6: "FAILED", 4: "FAILED"}[dhcp.messageType]
			emit(packet.at, "dhcp."+name, message, status)
			continue
		}
		events = append(events, tftpEvents(packet, client, clientIPs, transfers, deviceID)...)
	}
	if client == nil && len(clients) > 1 {
		return nil, fmt.Errorf("capture contains %d DHCP clients; select one with -client-mac", len(clients))
	}
	for _, transfer := range transfers {
		if !transfer.done && captureEnd.Sub(transfer.lastSeen) >= tftpIdleTimeout {
			events = append(events, Event{
				Timestamp: transfer.started.Format(time.RFC3339Nano), Name: "tftp.transfer.failed", Source: "capture", Service: "capture",
				DeviceID: deviceID, Status: "FAILED", Detail: transfer.file,
				Message: fmt.Sprintf("TFTP transfer of %s to %s did not complete on the wire (last acknowledged block %d)", transfer.file, transfer.clientIP, transfer.lastBlock),
			})
		}
	}
	return events, nil
}

func tftpEvents(packet udpPacket, client net.HardwareAddr, clientIPs map[string]bool, transfers map[string]*tftpTransfer, deviceID string) []Event {
	if len(packet.payload) < 4 {
		return nil
	}
	opcode := binary.BigEndian.Uint16(packet.payload)
	event := func(name, status, detail, message string) []Event {
		return []Event{{Timestamp: packet.at.Format(time.RFC3339Nano), Name: name, Source: "capture", Service: "capture",
			DeviceID: deviceID, Status: status, Detail: detail, Message: message, Raw: message}}
	}
	clientKey := func(ip net.IP, port uint16) string { return fmt.Sprintf("%s:%d", ip, port) }
	switch {
	case opcode == 1 && packet.dstPort == 69:
		if client != nil && !bytes.Equal(packet.srcMAC, client) && !clientIPs[packet.srcIP.String()] {
			return nil
		}
		file, _, _ := strings.Cut(string(packet.payload[2:]), "\x00")
		transfers[clientKey(packet.srcIP, packet.srcPort)] = &tftpTransfer{file: file, clientIP: packet.srcIP.String(), started: packet.at, lastSeen: packet.at}
		clientIPs[packet.srcIP.String()] = true
		return event("tftp.rrq", "RECEIVED", file, fmt.Sprintf("TFTP read request on the wire: %s from %s", file, packet.srcIP))
	case opcode == 3:
		transfer := transfers[clientKey(packet.dstIP, packet.dstPort)]
		if transfer != nil {
			transfer.lastSeen = packet.at
			transfer.lastBlock = binary.BigEndian.Uint16(packet.payload[2:])
			transfer.finalData = len(packet.payload)-4 < 512
		}
	case opcode == 4:
		transfer := transfers[clientKey(packet.srcIP, packet.srcPort)]
		if transfer != nil {
			transfer.lastSeen = packet.at
		}
		if transfer != nil && transfer.finalData && !transfer.done && binary.BigEndian.Uint16(packet.payload[2:]) == transfer.lastBlock {
			transfer.done = true
			return event("tftp.transfer.completed", "COMPLETED", transfer.file,
				fmt.Sprintf("TFTP transfer of %s to %s completed on the wire (%d blocks acknowledged)", transfer.file, transfer.clientIP, transfer.lastBlock))
		}
	case opcode == 5:
		transfer := transfers[clientKey(packet.dstIP, packet.dstPort)]
		if transfer == nil || transfer.done {
			return nil
		}
		transfer.done = true
		code := binary.BigEndian.Uint16(packet.payload[2:])
		text, _, _ := strings.Cut(string(packet.payload[4:]), "\x00")
		if code == 1 {
			return event("tftp.file_missing", "FAILED", transfer.file,
				fmt.Sprintf("TFTP server answered file not found for %s on the wire: %s", transfer.file, text))
		}
		return event("tftp.transfer.failed", "FAILED", transfer.file,
			fmt.Sprintf("TFTP error %d for %s on the wire: %s", code, transfer.file, text))
	}
	return nil
}
