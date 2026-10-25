// Package artnet implements the small part of Art-Net 4 the controller needs:
// sending ArtDmx frames, sending ArtPoll and parsing ArtPollReply.
package artnet

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Port is the standard Art-Net UDP port.
const Port = 6454

const (
	OpPoll      = 0x2000
	OpPollReply = 0x2100
	OpDmx       = 0x5000

	protocolVersion = 14
)

var header = []byte("Art-Net\x00")

// DmxPacket builds an ArtDmx packet for a 15-bit port address.
// data is padded to an even length of at least 2 and truncated to 512.
func DmxPacket(universe int, sequence byte, data []byte) []byte {
	if len(data) > 512 {
		data = data[:512]
	}
	n := len(data)
	if n < 2 {
		n = 2
	}
	if n%2 == 1 {
		n++
	}
	pkt := make([]byte, 18+n)
	copy(pkt, header)
	binary.LittleEndian.PutUint16(pkt[8:], OpDmx)
	binary.BigEndian.PutUint16(pkt[10:], protocolVersion)
	pkt[12] = sequence
	pkt[13] = 0                            // physical port
	pkt[14] = byte(universe & 0xff)        // SubUni
	pkt[15] = byte((universe >> 8) & 0x7f) // Net
	binary.BigEndian.PutUint16(pkt[16:], uint16(n))
	copy(pkt[18:], data)
	return pkt
}

// PollPacket builds an ArtPoll packet asking nodes to reply.
func PollPacket() []byte {
	pkt := make([]byte, 14)
	copy(pkt, header)
	binary.LittleEndian.PutUint16(pkt[8:], OpPoll)
	binary.BigEndian.PutUint16(pkt[10:], protocolVersion)
	pkt[12] = 0x02 // send ArtPollReply when conditions change
	pkt[13] = 0
	return pkt
}

// Node is a device that answered an ArtPoll.
type Node struct {
	IP        string    `json:"ip"`
	ShortName string    `json:"short_name"`
	LongName  string    `json:"long_name"`
	LastSeen  time.Time `json:"last_seen"`
}

// ParsePollReply decodes the fields of an ArtPollReply we care about.
func ParsePollReply(pkt []byte) (Node, error) {
	if len(pkt) < 108 || !bytes.Equal(pkt[:8], header) {
		return Node{}, errors.New("not an ArtPollReply")
	}
	if binary.LittleEndian.Uint16(pkt[8:]) != OpPollReply {
		return Node{}, errors.New("not an ArtPollReply")
	}
	return Node{
		IP:        net.IPv4(pkt[10], pkt[11], pkt[12], pkt[13]).String(),
		ShortName: cString(pkt[26:44]),
		LongName:  cString(pkt[44:108]),
	}, nil
}

func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}

// Conn is a UDP socket that sends Art-Net and collects ArtPollReplies.
type Conn struct {
	udp *net.UDPConn
	// BoundPort reports whether we got UDP 6454. Without it, nodes that reply
	// to the Art-Net port instead of our source port will not be heard.
	BoundPort bool

	mu    sync.Mutex
	seq   map[int]byte
	nodes map[string]Node
}

// Listen opens the socket, preferring UDP 6454 and falling back to any port.
func Listen() (*Conn, error) {
	c := &Conn{seq: map[int]byte{}, nodes: map[string]Node{}}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{Port: Port})
	if err == nil {
		c.BoundPort = true
	} else {
		udp, err = net.ListenUDP("udp4", &net.UDPAddr{})
		if err != nil {
			return nil, fmt.Errorf("open UDP socket: %w", err)
		}
	}
	c.udp = udp
	go c.readLoop()
	return c, nil
}

func (c *Conn) Close() error { return c.udp.Close() }

func (c *Conn) readLoop() {
	buf := make([]byte, 1500)
	for {
		n, from, err := c.udp.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		node, err := ParsePollReply(buf[:n])
		if err != nil {
			continue
		}
		if node.IP == "0.0.0.0" && from != nil {
			node.IP = from.IP.String()
		}
		node.LastSeen = time.Now()
		c.mu.Lock()
		c.nodes[node.IP] = node
		c.mu.Unlock()
	}
}

// SendDmx sends one ArtDmx frame, keeping a sequence counter per universe.
func (c *Conn) SendDmx(target string, universe int, data []byte) error {
	addr, err := udpAddr(target)
	if err != nil {
		return err
	}
	c.mu.Lock()
	seq := c.seq[universe]%255 + 1
	c.seq[universe] = seq
	c.mu.Unlock()
	_, err = c.udp.WriteToUDP(DmxPacket(universe, seq, data), addr)
	return err
}

// Poll sends an ArtPoll to each address (typically the node and the subnet broadcast).
func (c *Conn) Poll(addrs ...string) error {
	var firstErr error
	for _, a := range addrs {
		addr, err := udpAddr(a)
		if err == nil {
			_, err = c.udp.WriteToUDP(PollPacket(), addr)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Nodes returns the nodes heard from, most recent first is not guaranteed.
func (c *Conn) Nodes() []Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Node, 0, len(c.nodes))
	for _, n := range c.nodes {
		out = append(out, n)
	}
	return out
}

// Seen reports whether ip answered an ArtPoll within the last window.
func (c *Conn) Seen(ip string, window time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[ip]
	return ok && time.Since(n.LastSeen) < window
}

func udpAddr(ip string) (*net.UDPAddr, error) {
	parsed := net.ParseIP(strings.TrimSpace(ip)).To4()
	if parsed == nil {
		return nil, fmt.Errorf("invalid IPv4 address %q", ip)
	}
	return &net.UDPAddr{IP: parsed, Port: Port}, nil
}

// LocalIPv4s lists this machine's IPv4 addresses with their networks.
func LocalIPv4s() []*net.IPNet {
	var out []*net.IPNet
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			out = append(out, ipn)
		}
	}
	return out
}

// RouteFor returns the local network that contains target, if any. That is
// the "is the USB Ethernet set to 2.x.x.x" check.
func RouteFor(target string, locals []*net.IPNet) *net.IPNet {
	ip := net.ParseIP(target).To4()
	if ip == nil {
		return nil
	}
	for _, n := range locals {
		if n.Contains(ip) {
			return n
		}
	}
	return nil
}

// Broadcast returns the directed broadcast address of a network.
func Broadcast(n *net.IPNet) string {
	ip := n.IP.To4()
	mask := n.Mask
	if len(mask) == 16 {
		mask = mask[12:]
	}
	out := make(net.IP, 4)
	for i := range out {
		out[i] = ip[i] | ^mask[i]
	}
	return out.String()
}
