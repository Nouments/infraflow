package dhcp

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

type packetConn struct {
	written []byte
}

func (conn *packetConn) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, errors.New("unused") }
func (conn *packetConn) WriteTo(data []byte, _ net.Addr) (int, error) {
	conn.written = append([]byte(nil), data...)
	return len(data), nil
}
func (conn *packetConn) Close() error                     { return nil }
func (conn *packetConn) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (conn *packetConn) SetDeadline(time.Time) error      { return nil }
func (conn *packetConn) SetReadDeadline(time.Time) error  { return nil }
func (conn *packetConn) SetWriteDeadline(time.Time) error { return nil }

func validConfig() Config {
	return Config{
		Version: 1, Service: "dhcp", Enabled: true, Site: "lab",
		Network: "192.168.50.0/24", Gateway: "192.168.50.1",
		Pools:             []Pool{{Start: "192.168.50.20", End: "192.168.50.21"}},
		ReservedAddresses: []string{"192.168.50.1", "192.168.50.10"},
		Reservations:      []Reservation{{MAC: "00:11:22:33:44:55", IP: "192.168.50.10", Hostname: "router"}},
		Options:           ConfigOptions{DNS: []string{"192.168.50.2"}, NextServerSource: "agent_runtime", BootFilename: "undionly.kpxe", TFTPDirectory: "tftp"},
	}
}

func TestHandleOffersReservationAndAcknowledgesRequestedLease(t *testing.T) {
	server, err := NewServer(validConfig(), net.ParseIP("192.168.50.2"))
	if err != nil {
		t.Fatal(err)
	}
	mac, _ := net.ParseMAC("00:11:22:33:44:55")
	conn := &packetConn{}
	peer := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	server.Handle(conn, peer, makePacket(t, dhcpv4.MessageTypeDiscover, mac))
	offer := decodeResponse(t, conn.written)
	if offer.MessageType() != dhcpv4.MessageTypeOffer || !offer.YourIPAddr.Equal(net.ParseIP("192.168.50.10")) {
		t.Fatalf("unexpected reservation offer: %s", offer.Summary())
	}
	if offer.TFTPServerName() != "192.168.50.2" || offer.BootFileNameOption() != "undionly.kpxe" || !offer.ServerIPAddr.Equal(net.ParseIP("192.168.50.2")) {
		t.Fatalf("DHCP offer omitted next-server or options 66/67: %s", offer.Summary())
	}

	conn.written = nil
	request := makePacket(t, dhcpv4.MessageTypeRequest, mac,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(net.ParseIP("192.168.50.10"))),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("192.168.50.2"))),
	)
	server.Handle(conn, peer, request)
	ack := decodeResponse(t, conn.written)
	if ack.MessageType() != dhcpv4.MessageTypeAck || !ack.YourIPAddr.Equal(net.ParseIP("192.168.50.10")) {
		t.Fatalf("unexpected reservation acknowledgement: %s", ack.Summary())
	}
	if !net.IP(ack.SubnetMask()).Equal(net.IP(net.CIDRMask(24, 32))) || len(ack.Router()) != 1 || !ack.Router()[0].Equal(net.ParseIP("192.168.50.1")) || len(ack.DNS()) != 1 || ack.HostName() != "router" {
		t.Fatalf("network options missing from ACK: %s", ack.Summary())
	}
}

func TestHandleAllocatesPoolAndRejectsAddressOutsidePool(t *testing.T) {
	server, err := NewServer(validConfig(), net.ParseIP("192.168.50.2"))
	if err != nil {
		t.Fatal(err)
	}
	mac, _ := net.ParseMAC("00:11:22:33:44:66")
	conn := &packetConn{}
	peer := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	server.Handle(conn, peer, makePacket(t, dhcpv4.MessageTypeDiscover, mac))
	offer := decodeResponse(t, conn.written)
	if offer.MessageType() != dhcpv4.MessageTypeOffer || !offer.YourIPAddr.Equal(net.ParseIP("192.168.50.20")) {
		t.Fatalf("unexpected dynamic offer: %s", offer.Summary())
	}

	conn.written = nil
	request := makePacket(t, dhcpv4.MessageTypeRequest, mac,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(net.ParseIP("192.168.50.30"))),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("192.168.50.2"))),
	)
	server.Handle(conn, peer, request)
	if response := decodeResponse(t, conn.written); response.MessageType() != dhcpv4.MessageTypeNak {
		t.Fatalf("out-of-pool request was not rejected: %s", response.Summary())
	}
}

func TestHandleIgnoresRequestsForAnotherServer(t *testing.T) {
	server, err := NewServer(validConfig(), net.ParseIP("192.168.50.2"))
	if err != nil {
		t.Fatal(err)
	}
	mac, _ := net.ParseMAC("00:11:22:33:44:66")
	conn := &packetConn{}
	request := makePacket(t, dhcpv4.MessageTypeRequest, mac,
		dhcpv4.WithOption(dhcpv4.OptRequestedIPAddress(net.ParseIP("192.168.50.20"))),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("192.168.50.3"))),
	)
	server.Handle(conn, &net.UDPAddr{IP: net.IPv4bcast, Port: 68}, request)
	if len(conn.written) != 0 {
		t.Fatal("request selecting another DHCP server received a response")
	}
}

func TestConcurrentOffersAllocateUniqueAddresses(t *testing.T) {
	config := validConfig()
	config.Pools = []Pool{{Start: "192.168.50.20", End: "192.168.50.30"}}
	server, err := NewServer(config, net.ParseIP("192.168.50.2"))
	if err != nil {
		t.Fatal(err)
	}
	const clients = 10
	type offerResult struct {
		address string
		err     error
	}
	addresses := make(chan offerResult, clients)
	var wait sync.WaitGroup
	for client := 0; client < clients; client++ {
		wait.Add(1)
		go func(client int) {
			defer wait.Done()
			mac := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, byte(client + 1)}
			packet, err := dhcpv4.New(dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover), dhcpv4.WithHwAddr(mac))
			if err != nil {
				addresses <- offerResult{err: err}
				return
			}
			conn := &packetConn{}
			server.Handle(conn, &net.UDPAddr{IP: net.IPv4bcast, Port: 68}, packet)
			if len(conn.written) == 0 {
				addresses <- offerResult{err: errors.New("server did not send a DHCP response")}
				return
			}
			response, err := dhcpv4.FromBytes(conn.written)
			if err != nil {
				addresses <- offerResult{err: err}
				return
			}
			addresses <- offerResult{address: response.YourIPAddr.String()}
		}(client)
	}
	wait.Wait()
	close(addresses)
	seen := make(map[string]struct{}, clients)
	for result := range addresses {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if _, exists := seen[result.address]; exists {
			t.Fatalf("server offered duplicate address %s", result.address)
		}
		seen[result.address] = struct{}{}
	}
	if len(seen) != clients {
		t.Fatalf("expected %d unique addresses, got %d", clients, len(seen))
	}
}

func TestValidateRejectsOverlappingPoolsAndReservations(t *testing.T) {
	config := validConfig()
	config.Pools[0] = Pool{Start: "192.168.50.10", End: "192.168.50.21"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected pool overlapping a reservation to fail")
	}
	config = validConfig()
	config.Pools = append(config.Pools, Pool{Start: "192.168.50.21", End: "192.168.50.25"})
	if err := config.Validate(); err == nil {
		t.Fatal("expected overlapping pools to fail")
	}
}

func TestValidateRejectsReservationMatchingGateway(t *testing.T) {
	config := validConfig()
	config.Reservations[0].IP = "192.168.50.1"
	if err := config.Validate(); err == nil {
		t.Fatal("expected reservation overlapping the gateway to fail")
	}
}

func TestNewServerRequiresEnabledConfigAndUnallocatableListenAddress(t *testing.T) {
	config := validConfig()
	config.Enabled = false
	if _, err := NewServer(config, net.ParseIP("192.168.50.2")); err == nil {
		t.Fatal("expected disabled service to be rejected")
	}
	config = validConfig()
	if _, err := NewServer(config, net.ParseIP("192.168.50.20")); err == nil {
		t.Fatal("expected listen address inside an assignable pool to be rejected")
	}
	if _, err := NewServer(config, net.ParseIP("192.168.50.0")); err == nil {
		t.Fatal("expected network address to be rejected as a listen address")
	}
	if _, err := NewServer(config, net.ParseIP("192.168.50.10")); err == nil {
		t.Fatal("expected reserved address to be rejected as a listen address")
	}
}

func TestValidateRejectsInvalidNextServer(t *testing.T) {
	config := validConfig()
	config.Options.NextServer = "not-an-ip"
	if err := config.Validate(); err == nil {
		t.Fatal("expected invalid next-server address to fail")
	}
}

func makePacket(t *testing.T, messageType dhcpv4.MessageType, mac net.HardwareAddr, modifiers ...dhcpv4.Modifier) *dhcpv4.DHCPv4 {
	t.Helper()
	allModifiers := []dhcpv4.Modifier{dhcpv4.WithMessageType(messageType), dhcpv4.WithHwAddr(mac)}
	allModifiers = append(allModifiers, modifiers...)
	packet, err := dhcpv4.New(allModifiers...)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func decodeResponse(t *testing.T, data []byte) *dhcpv4.DHCPv4 {
	t.Helper()
	if len(data) == 0 {
		t.Fatal("server did not send a DHCP response")
	}
	packet, err := dhcpv4.FromBytes(data)
	if err != nil {
		t.Fatalf("decode DHCP response: %v", err)
	}
	return packet
}
