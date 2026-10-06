package dhcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"infraflow/pkg/protocol"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
)

const (
	maxConfigBytes  = 1 << 20
	defaultLease    = 10 * time.Minute
	offerLifetime   = 2 * time.Minute
	declineHold     = 10 * time.Minute
	maxMemoryLeases = 1 << 16
)

type Config struct {
	Version           int           `json:"version"`
	Service           string        `json:"service"`
	Enabled           bool          `json:"enabled"`
	Site              string        `json:"site"`
	Network           string        `json:"network"`
	Gateway           string        `json:"gateway"`
	Pools             []Pool        `json:"pools"`
	ReservedAddresses []string      `json:"reserved_addresses"`
	Reservations      []Reservation `json:"reservations"`
	Options           ConfigOptions `json:"options"`
}

type Pool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Reservation struct {
	MAC      string `json:"mac"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}

type ConfigOptions struct {
	DNS              []string `json:"dns_servers"`
	NextServer       string   `json:"next_server"`
	NextServerSource string   `json:"next_server_source"`
	BootMode         string   `json:"boot_mode"`
	BootFilename     string   `json:"boot_filename"`
	TFTPDirectory    string   `json:"tftp_directory"`
	IPXEScript       string   `json:"ipxe_script"`
}

type lease struct {
	ip      net.IP
	expires time.Time
}

type Server struct {
	config       Config
	network      *net.IPNet
	serverIP     net.IP
	reservations map[string]Reservation
	leasesByID   map[string]lease
	leaseOwners  map[string]string
	declined     map[string]time.Time
	mu           sync.Mutex
}

func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open DHCP config: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("read DHCP config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("DHCP config exceeds %d bytes", maxConfigBytes)
	}
	var config Config
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode DHCP config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("DHCP config must contain exactly one JSON document")
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if config.Version != 1 || config.Service != "dhcp" {
		return fmt.Errorf("DHCP config must use version 1 and service dhcp")
	}
	if !protocol.ValidSiteName(config.Site) {
		return fmt.Errorf("DHCP config has an invalid site name")
	}
	_, network, err := net.ParseCIDR(config.Network)
	if err != nil || network.IP.To4() == nil {
		return fmt.Errorf("DHCP network must be a valid IPv4 CIDR")
	}
	if !network.IP.Equal(net.ParseIP(config.Network[:strings.IndexByte(config.Network, '/')]).To4()) {
		return fmt.Errorf("DHCP network must use its canonical network address")
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || ones >= 31 {
		return fmt.Errorf("DHCP network must contain usable IPv4 host addresses")
	}
	first := ipNumber(network.IP) + 1
	last := ipNumber(network.IP) + (1 << uint(bits-ones)) - 2
	reserved := make(map[uint32]string)
	if config.Gateway != "" {
		gateway := net.ParseIP(config.Gateway).To4()
		if gateway == nil || !network.Contains(gateway) || ipNumber(gateway) < first || ipNumber(gateway) > last {
			return fmt.Errorf("DHCP gateway must be a usable address inside the network")
		}
		reserved[ipNumber(gateway)] = "gateway"
	}
	for _, raw := range config.ReservedAddresses {
		ip := net.ParseIP(raw).To4()
		if ip == nil || !network.Contains(ip) || ipNumber(ip) < first || ipNumber(ip) > last {
			return fmt.Errorf("reserved DHCP address %q is not a usable address inside the network", raw)
		}
		if _, exists := reserved[ipNumber(ip)]; !exists {
			reserved[ipNumber(ip)] = "reserved address"
		}
	}
	seenMACs := make(map[string]struct{}, len(config.Reservations))
	seenIPs := make(map[uint32]struct{}, len(config.Reservations))
	for _, reservation := range config.Reservations {
		mac, err := net.ParseMAC(reservation.MAC)
		ip := net.ParseIP(reservation.IP).To4()
		if err != nil || len(mac) != 6 || ip == nil || !network.Contains(ip) || ipNumber(ip) < first || ipNumber(ip) > last {
			return fmt.Errorf("DHCP reservation must contain a valid Ethernet MAC and usable IPv4 address")
		}
		if _, exists := seenMACs[strings.ToLower(mac.String())]; exists {
			return fmt.Errorf("DHCP reservations contain a duplicate MAC address")
		}
		if _, exists := seenIPs[ipNumber(ip)]; exists {
			return fmt.Errorf("DHCP reservations contain a duplicate IP address")
		}
		seenMACs[strings.ToLower(mac.String())] = struct{}{}
		seenIPs[ipNumber(ip)] = struct{}{}
		if owner, exists := reserved[ipNumber(ip)]; exists && owner != "reserved address" {
			return fmt.Errorf("DHCP reservation overlaps the %s address", owner)
		}
		reserved[ipNumber(ip)] = "reservation"
	}
	for _, dns := range config.Options.DNS {
		if net.ParseIP(dns).To4() == nil {
			return fmt.Errorf("DHCP DNS server %q must be an IPv4 address", dns)
		}
	}
	if config.Options.NextServer != "" && net.ParseIP(config.Options.NextServer).To4() == nil {
		return fmt.Errorf("DHCP next_server must be an IPv4 address")
	}
	if source := config.Options.NextServerSource; source != "" && source != "agent_runtime" && net.ParseIP(source).To4() == nil {
		return fmt.Errorf("DHCP next_server_source must be agent_runtime or an IPv4 address")
	}
	for _, pool := range config.Pools {
		start := net.ParseIP(pool.Start).To4()
		end := net.ParseIP(pool.End).To4()
		if start == nil || end == nil || !network.Contains(start) || !network.Contains(end) || ipNumber(start) < first || ipNumber(end) > last || ipNumber(start) > ipNumber(end) {
			return fmt.Errorf("DHCP pool %q-%q is not a valid usable range inside the network", pool.Start, pool.End)
		}
		for address, owner := range reserved {
			if address >= ipNumber(start) && address <= ipNumber(end) {
				return fmt.Errorf("DHCP pool overlaps %s address %s", owner, uintIP(address))
			}
		}
	}
	pools := append([]Pool(nil), config.Pools...)
	sort.Slice(pools, func(i, j int) bool {
		return ipNumber(net.ParseIP(pools[i].Start).To4()) < ipNumber(net.ParseIP(pools[j].Start).To4())
	})
	for index := 1; index < len(pools); index++ {
		if ipNumber(net.ParseIP(pools[index].Start).To4()) <= ipNumber(net.ParseIP(pools[index-1].End).To4()) {
			return fmt.Errorf("DHCP pools overlap")
		}
	}
	return nil
}

func NewServer(config Config, serverIP net.IP) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return nil, fmt.Errorf("DHCP service is disabled in the generated config")
	}
	ip := serverIP.To4()
	_, network, _ := net.ParseCIDR(config.Network)
	ones, bits := network.Mask.Size()
	first := ipNumber(network.IP) + 1
	last := ipNumber(network.IP) + (1 << uint(bits-ones)) - 2
	if ip == nil || !network.Contains(ip) || ipNumber(ip) < first || ipNumber(ip) > last {
		return nil, fmt.Errorf("DHCP listen address must be a usable IPv4 address inside the configured network")
	}
	for _, pool := range config.Pools {
		if ipNumber(ip) >= ipNumber(net.ParseIP(pool.Start).To4()) && ipNumber(ip) <= ipNumber(net.ParseIP(pool.End).To4()) {
			return nil, fmt.Errorf("DHCP listen address must not be inside an assignable pool")
		}
	}
	for _, reservation := range config.Reservations {
		if net.ParseIP(reservation.IP).To4().Equal(ip) {
			return nil, fmt.Errorf("DHCP listen address must not overlap a device reservation")
		}
	}
	reservations := make(map[string]Reservation, len(config.Reservations))
	for _, reservation := range config.Reservations {
		mac, _ := net.ParseMAC(reservation.MAC)
		reservations[strings.ToLower(mac.String())] = reservation
	}
	return &Server{
		config: config, network: network, serverIP: ip,
		reservations: reservations, leasesByID: make(map[string]lease),
		leaseOwners: make(map[string]string), declined: make(map[string]time.Time),
	}, nil
}

func (server *Server) Serve(ctx context.Context, interfaceName string, port int) error {
	if ctx == nil || strings.TrimSpace(interfaceName) == "" || port < 1 || port > 65535 {
		return fmt.Errorf("DHCP context, interface, and valid UDP port are required")
	}
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return fmt.Errorf("find DHCP interface: %w", err)
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return fmt.Errorf("read DHCP interface addresses: %w", err)
	}
	found := false
	for _, address := range addresses {
		localIP, _, parseErr := net.ParseCIDR(address.String())
		if parseErr == nil && localIP.To4() != nil && localIP.Equal(server.serverIP) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("DHCP listen address %s is not assigned to interface %s", server.serverIP, interfaceName)
	}
	listener, err := server4.NewServer(interfaceName, &net.UDPAddr{IP: server.serverIP, Port: port}, server.Handle)
	if err != nil {
		return fmt.Errorf("listen for DHCP on %s/%s:%d: %w", interfaceName, server.serverIP, port, err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- listener.Serve() }()
	select {
	case <-ctx.Done():
		_ = listener.Close()
		err := <-serveDone
		if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
			return nil
		}
		return err
	case err := <-serveDone:
		if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}

func (server *Server) Handle(conn net.PacketConn, peer net.Addr, request *dhcpv4.DHCPv4) {
	if conn == nil || peer == nil || request == nil || len(request.ClientHWAddr) != 6 {
		return
	}
	clientID := clientKey(request)
	if clientID == "" {
		return
	}
	var response *dhcpv4.DHCPv4
	switch request.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		address, ok := server.offer(clientID, request.ClientHWAddr)
		if !ok {
			return
		}
		response, _ = dhcpv4.NewReplyFromRequest(request,
			dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
			dhcpv4.WithYourIP(address),
		)
	case dhcpv4.MessageTypeRequest:
		if request.ServerIdentifier() != nil && !request.ServerIdentifier().Equal(server.serverIP) {
			return
		}
		requestedIP := request.RequestedIPAddress()
		if requestedIP == nil || requestedIP.To4() == nil {
			requestedIP = request.ClientIPAddr.To4()
		}
		if requestedIP == nil || requestedIP.Equal(net.IPv4zero) {
			return
		}
		accepted := server.request(clientID, request.ClientHWAddr, requestedIP)
		messageType := dhcpv4.MessageTypeNak
		if accepted {
			messageType = dhcpv4.MessageTypeAck
		}
		modifiers := []dhcpv4.Modifier{
			dhcpv4.WithMessageType(messageType),
			dhcpv4.WithServerIP(server.serverIP),
			dhcpv4.WithOption(dhcpv4.OptServerIdentifier(server.serverIP)),
		}
		if accepted {
			modifiers = append(modifiers, dhcpv4.WithYourIP(requestedIP))
		}
		response, _ = dhcpv4.NewReplyFromRequest(request, modifiers...)
	case dhcpv4.MessageTypeRelease:
		server.release(clientID, request.ClientHWAddr, request.ClientIPAddr)
		return
	case dhcpv4.MessageTypeDecline:
		server.decline(clientID, request.ClientHWAddr, request.RequestedIPAddress())
		return
	default:
		return
	}
	if response == nil {
		return
	}
	if response.MessageType() != dhcpv4.MessageTypeNak {
		server.addNetworkOptions(response)
		if reservation, exists := server.reservations[strings.ToLower(request.ClientHWAddr.String())]; exists && reservation.Hostname != "" {
			response.UpdateOption(dhcpv4.OptHostName(reservation.Hostname))
		}
	}
	if _, err := conn.WriteTo(response.ToBytes(), peer); err != nil {
		return
	}
}

func (server *Server) addNetworkOptions(response *dhcpv4.DHCPv4) {
	response.UpdateOption(dhcpv4.OptServerIdentifier(server.serverIP))
	response.UpdateOption(dhcpv4.OptSubnetMask(server.network.Mask))
	response.UpdateOption(dhcpv4.OptIPAddressLeaseTime(defaultLease))
	if server.config.Gateway != "" {
		response.UpdateOption(dhcpv4.OptRouter(net.ParseIP(server.config.Gateway).To4()))
	}
	dns := make([]net.IP, 0, len(server.config.Options.DNS))
	for _, address := range server.config.Options.DNS {
		dns = append(dns, net.ParseIP(address).To4())
	}
	if len(dns) > 0 {
		response.UpdateOption(dhcpv4.OptDNS(dns...))
	}
	if server.config.Options.BootFilename != "" {
		response.UpdateOption(dhcpv4.OptBootFileName(server.config.Options.BootFilename))
	}
	nextServer := server.serverIP
	if configuredAddress := server.config.Options.NextServer; configuredAddress != "" {
		nextServer = net.ParseIP(configuredAddress).To4()
	} else if sourceAddress := server.config.Options.NextServerSource; sourceAddress != "" && sourceAddress != "agent_runtime" {
		nextServer = net.ParseIP(sourceAddress).To4()
	}
	response.ServerIPAddr = nextServer
	if server.config.Options.TFTPDirectory != "" {
		response.UpdateOption(dhcpv4.OptTFTPServerName(nextServer.String()))
	}
}

func (server *Server) offer(clientID string, hardwareAddress net.HardwareAddr) (net.IP, bool) {
	server.mu.Lock()
	defer server.mu.Unlock()
	now := time.Now()
	server.expire(now)
	if reservation, exists := server.reservations[strings.ToLower(hardwareAddress.String())]; exists {
		return net.ParseIP(reservation.IP).To4(), true
	}
	if existing, exists := server.leasesByID[clientID]; exists {
		return existing.ip, true
	}
	if len(server.leasesByID) >= maxMemoryLeases {
		return nil, false
	}
	for _, pool := range server.config.Pools {
		start := ipNumber(net.ParseIP(pool.Start).To4())
		end := ipNumber(net.ParseIP(pool.End).To4())
		for candidate := start; candidate <= end; candidate++ {
			ip := uintIP(candidate)
			key := ip.String()
			if owner, exists := server.leaseOwners[key]; exists && owner != clientID {
				continue
			}
			if until, exists := server.declined[key]; exists && now.Before(until) {
				continue
			}
			server.leasesByID[clientID] = lease{ip: ip, expires: now.Add(offerLifetime)}
			server.leaseOwners[key] = clientID
			return ip, true
		}
	}
	return nil, false
}

func (server *Server) request(clientID string, hardwareAddress net.HardwareAddr, requestedIP net.IP) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	now := time.Now()
	server.expire(now)
	if reservation, exists := server.reservations[strings.ToLower(hardwareAddress.String())]; exists {
		if !net.ParseIP(reservation.IP).To4().Equal(requestedIP.To4()) {
			return false
		}
		return true
	}
	if !server.inPool(requestedIP) {
		return false
	}
	ipKey := requestedIP.String()
	if until, exists := server.declined[ipKey]; exists && now.Before(until) {
		return false
	}
	if owner, exists := server.leaseOwners[ipKey]; exists && owner != clientID {
		return false
	}
	if current, exists := server.leasesByID[clientID]; exists && !current.ip.Equal(requestedIP) {
		delete(server.leaseOwners, current.ip.String())
	}
	server.leasesByID[clientID] = lease{ip: requestedIP.To4(), expires: now.Add(defaultLease)}
	server.leaseOwners[ipKey] = clientID
	return true
}

func (server *Server) release(clientID string, hardwareAddress net.HardwareAddr, address net.IP) {
	server.mu.Lock()
	defer server.mu.Unlock()
	_, isReserved := server.reservations[strings.ToLower(hardwareAddress.String())]
	if isReserved || address == nil {
		return
	}
	if current, exists := server.leasesByID[clientID]; exists && current.ip.Equal(address.To4()) {
		delete(server.leaseOwners, current.ip.String())
		delete(server.leasesByID, clientID)
	}
}

func (server *Server) decline(clientID string, hardwareAddress net.HardwareAddr, address net.IP) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if _, reserved := server.reservations[strings.ToLower(hardwareAddress.String())]; reserved || address == nil || !server.inPool(address) {
		return
	}
	ipKey := address.To4().String()
	if owner, exists := server.leaseOwners[ipKey]; exists && owner == clientID {
		delete(server.leaseOwners, ipKey)
		delete(server.leasesByID, clientID)
		server.declined[ipKey] = time.Now().Add(declineHold)
	}
}

func (server *Server) expire(now time.Time) {
	for clientID, current := range server.leasesByID {
		if !now.Before(current.expires) {
			delete(server.leaseOwners, current.ip.String())
			delete(server.leasesByID, clientID)
		}
	}
	for address, until := range server.declined {
		if !now.Before(until) {
			delete(server.declined, address)
		}
	}
}

func (server *Server) inPool(address net.IP) bool {
	ip := address.To4()
	if ip == nil || !server.network.Contains(ip) {
		return false
	}
	value := ipNumber(ip)
	for _, pool := range server.config.Pools {
		if value >= ipNumber(net.ParseIP(pool.Start).To4()) && value <= ipNumber(net.ParseIP(pool.End).To4()) {
			return true
		}
	}
	return false
}

func clientKey(request *dhcpv4.DHCPv4) string {
	if identifier := request.GetOneOption(dhcpv4.OptionClientIdentifier); len(identifier) > 0 {
		return "id:" + string(identifier)
	}
	return "mac:" + strings.ToLower(request.ClientHWAddr.String())
}

func ipNumber(ip net.IP) uint32 {
	value := ip.To4()
	return uint32(value[0])<<24 | uint32(value[1])<<16 | uint32(value[2])<<8 | uint32(value[3])
}

func uintIP(value uint32) net.IP {
	return net.IPv4(byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
}
