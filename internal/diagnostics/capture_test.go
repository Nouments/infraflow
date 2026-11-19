package diagnostics

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	testClientMAC = net.HardwareAddr{0x0c, 0xeb, 0x25, 0xa0, 0x00, 0x00}
	testServerMAC = net.HardwareAddr{0x02, 0x42, 0xd1, 0x65, 0xdb, 0x00}
	testOtherMAC  = net.HardwareAddr{0xca, 0x01, 0x43, 0xc9, 0x00, 0x00}
	testServerIP  = net.IPv4(192, 0, 2, 2).To4()
	testClientIP  = net.IPv4(192, 0, 2, 11).To4()
)

type pcapWriter struct {
	buf bytes.Buffer
	at  time.Time
}

func newPcap() *pcapWriter {
	w := &pcapWriter{at: time.Date(2026, 11, 19, 15, 43, 28, 0, time.UTC)}
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(header[4:], 2)
	binary.LittleEndian.PutUint16(header[6:], 4)
	binary.LittleEndian.PutUint32(header[16:], 65535)
	binary.LittleEndian.PutUint32(header[20:], linkTypeEthernet)
	w.buf.Write(header)
	return w
}

func (w *pcapWriter) udp(srcMAC net.HardwareAddr, src, dst net.IP, srcPort, dstPort uint16, payload []byte) {
	w.at = w.at.Add(time.Second)
	frame := make([]byte, 0, 42+len(payload))
	frame = append(frame, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	frame = append(frame, srcMAC...)
	frame = append(frame, 0x08, 0x00)
	ip := make([]byte, 20)
	ip[0], ip[8], ip[9] = 0x45, 64, 17
	binary.BigEndian.PutUint16(ip[2:], uint16(28+len(payload)))
	copy(ip[12:], src.To4())
	copy(ip[16:], dst.To4())
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp, srcPort)
	binary.BigEndian.PutUint16(udp[2:], dstPort)
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(payload)))
	frame = append(append(append(frame, ip...), udp...), payload...)
	record := make([]byte, 16)
	binary.LittleEndian.PutUint32(record, uint32(w.at.Unix()))
	binary.LittleEndian.PutUint32(record[8:], uint32(len(frame)))
	binary.LittleEndian.PutUint32(record[12:], uint32(len(frame)))
	w.buf.Write(record)
	w.buf.Write(frame)
}

func dhcpPayload(messageType byte, client net.HardwareAddr, yiaddr net.IP, options ...[]byte) []byte {
	payload := make([]byte, 240)
	payload[0], payload[1], payload[2] = 1, 1, 6
	if messageType == 2 || messageType == 5 || messageType == 6 {
		payload[0] = 2
	}
	if yiaddr != nil {
		copy(payload[16:], yiaddr.To4())
	}
	copy(payload[28:], client)
	copy(payload[236:], []byte{99, 130, 83, 99})
	payload = append(payload, 53, 1, messageType)
	for _, option := range options {
		payload = append(payload, option...)
	}
	return append(payload, 255)
}

func serverID(ip net.IP) []byte { return append([]byte{54, 4}, ip.To4()...) }

func (w *pcapWriter) dhcp(messageType byte, client net.HardwareAddr) {
	switch messageType {
	case 2, 5, 6:
		w.udp(testServerMAC, testServerIP, net.IPv4bcast, 67, 68, dhcpPayload(messageType, client, testClientIP, serverID(testServerIP)))
	default:
		w.udp(client, net.IPv4zero, net.IPv4bcast, 68, 67, dhcpPayload(messageType, client, nil, serverID(testServerIP)))
	}
}

func (w *pcapWriter) tftp(src, dst net.IP, srcPort, dstPort uint16, opcode uint16, body []byte) {
	payload := binary.BigEndian.AppendUint16(nil, opcode)
	mac := testClientMAC
	if src.Equal(testServerIP) {
		mac = testServerMAC
	}
	w.udp(mac, src, dst, srcPort, dstPort, append(payload, body...))
}

func (w *pcapWriter) analyze(t *testing.T, clientMAC string) Report {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap.pcap")
	if err := os.WriteFile(path, w.buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	events, err := ReadCapture(CaptureSource{Path: path, ClientMAC: clientMAC, DeviceID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	return Analyze(events, Filter{DeviceID: "r1"})
}

func TestCaptureDistinguishesDHCPWireStates(t *testing.T) {
	cases := []struct {
		name       string
		messages   []byte
		code       string
		confidence string
	}{
		{"discover without offer", []byte{1, 1, 1}, "DHCP_DISCOVER_WITHOUT_OFFER", confidenceMed},
		{"offers never requested", []byte{1, 2, 1, 2, 1, 2}, "DHCP_OFFER_NOT_ACCEPTED", confidenceHigh},
		{"request without ack", []byte{1, 2, 3, 3}, "DHCP_REQUEST_WITHOUT_ACK", confidenceHigh},
		{"nak", []byte{1, 2, 3, 6}, "DHCP_NAK", confidenceHigh},
		{"decline after ack", []byte{1, 2, 3, 4}, "DHCP_DECLINE", confidenceHigh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newPcap()
			for _, message := range tc.messages {
				w.dhcp(message, testClientMAC)
			}
			report := w.analyze(t, testClientMAC.String())
			if report.FailureCode != tc.code || report.FailureStage != StageDHCP {
				t.Fatalf("got %s/%s, want %s: %s", report.FailureStage, report.FailureCode, tc.code, report.LikelyCause)
			}
			if tc.code != "DHCP_DISCOVER_WITHOUT_OFFER" && report.Confidence != tc.confidence {
				t.Fatalf("confidence %s, want %s", report.Confidence, tc.confidence)
			}
		})
	}
}

func TestCaptureLeaseAndTFTPTransfer(t *testing.T) {
	w := newPcap()
	for _, message := range []byte{1, 2, 3, 5} {
		w.dhcp(message, testClientMAC)
	}
	// AutoInstall probes a missing file first, then fetches its own file.
	w.tftp(testClientIP, testServerIP, 50000, 69, 1, []byte("network-confg\x00octet\x00"))
	w.tftp(testServerIP, testClientIP, 40000, 50000, 5, []byte("\x00\x01File not found\x00"))
	w.tftp(testClientIP, testServerIP, 50001, 69, 1, []byte("r1-confg\x00octet\x00"))
	w.tftp(testServerIP, testClientIP, 40001, 50001, 3, append([]byte{0, 1}, bytes.Repeat([]byte("a"), 512)...))
	w.tftp(testClientIP, testServerIP, 50001, 40001, 4, []byte{0, 1})
	w.tftp(testServerIP, testClientIP, 40001, 50001, 3, append([]byte{0, 2}, []byte("end\n")...))
	w.tftp(testClientIP, testServerIP, 50001, 40001, 4, []byte{0, 2})
	report := w.analyze(t, testClientMAC.String())
	if report.FailureCode != "" || report.LastConfirmedSuccessfulStage != StageTFTP {
		t.Fatalf("lease and transfer not recognized: %+v", report)
	}
	var sawMissing, sawCompleted bool
	for _, evidence := range report.Evidence {
		sawMissing = sawMissing || (evidence.Code == "TFTP_FILE_MISSING" && strings.Contains(evidence.Message, "network-confg"))
		sawCompleted = sawCompleted || (evidence.Code == "TFTP_FILE_TRANSFERRED" && strings.Contains(evidence.Message, "2 blocks"))
	}
	if !sawMissing || !sawCompleted {
		t.Fatalf("missing wire evidence: %+v", report.Evidence)
	}
}

func TestCaptureReportsInterruptedTFTPTransfer(t *testing.T) {
	w := newPcap()
	for _, message := range []byte{1, 2, 3, 5} {
		w.dhcp(message, testClientMAC)
	}
	w.tftp(testClientIP, testServerIP, 50001, 69, 1, []byte("r1-confg\x00octet\x00"))
	w.tftp(testServerIP, testClientIP, 40001, 50001, 3, append([]byte{0, 1}, bytes.Repeat([]byte("a"), 512)...))
	// Capture stopped right away: not enough evidence to call it a failure.
	if report := w.analyze(t, testClientMAC.String()); report.FailureCode != "" || report.ObservedStatus != statusIncomplet {
		t.Fatalf("transfer cut by the end of the capture reported as %s", report.FailureCode)
	}
	// Capture kept running well past the TFTP timeout with no further block.
	w.at = w.at.Add(30 * time.Second)
	w.dhcp(1, testOtherMAC)
	report := w.analyze(t, testClientMAC.String())
	if report.FailureCode != "TFTP_TRANSFER_FAILED" {
		t.Fatalf("interrupted transfer not detected: %s %s", report.FailureCode, report.LikelyCause)
	}
}

func TestCaptureSelectsOneClient(t *testing.T) {
	w := newPcap()
	w.dhcp(1, testClientMAC)
	w.dhcp(1, testOtherMAC)
	for _, message := range []byte{2, 3, 5} {
		w.dhcp(message, testOtherMAC)
	}
	path := filepath.Join(t.TempDir(), "two.pcap")
	if err := os.WriteFile(path, w.buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCapture(CaptureSource{Path: path}); err == nil || !strings.Contains(err.Error(), "-client-mac") {
		t.Fatalf("ambiguous capture accepted: %v", err)
	}
	// The other client's lease must not make this client look successful.
	report := w.analyze(t, testClientMAC.String())
	if report.FailureCode != "DHCP_DISCOVER_WITHOUT_OFFER" {
		t.Fatalf("other client's packets leaked into the diagnosis: %s", report.FailureCode)
	}
}

func TestCaptureRejectsUnsupportedFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string][]byte{
		"pcapng":    {0x0a, 0x0d, 0x0d, 0x0a, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		"text":      []byte(strings.Repeat("not a capture\n", 3)),
		"truncated": {0xd4, 0xc3},
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCapture(CaptureSource{Path: path}); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := ReadCapture(CaptureSource{Path: filepath.Join(dir, "text"), ClientMAC: "zz"}); err == nil {
		t.Fatal("invalid MAC accepted")
	}
}

// Malformed packets must never panic the parser.
func FuzzCaptureParser(f *testing.F) {
	w := newPcap()
	w.dhcp(1, testClientMAC)
	w.tftp(testClientIP, testServerIP, 50001, 69, 1, []byte("r1-confg\x00octet\x00"))
	f.Add(w.buf.Bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		packets, err := readPcap(bytes.NewReader(data))
		if err != nil {
			return
		}
		_, _ = captureEvents(packets, testClientMAC, "r1")
	})
}
