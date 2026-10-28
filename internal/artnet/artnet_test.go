package artnet

import (
	"bytes"
	"net"
	"testing"
)

// Matches the packet built by the original artnet.py probe script.
func TestDmxPacketMatchesProbeScript(t *testing.T) {
	data := make([]byte, 512)
	data[16] = 255
	pkt := DmxPacket(0, 1, data)
	want := append([]byte("Art-Net\x00\x00\x50\x00\x0e\x01\x00\x00\x00\x02\x00"), data...)
	if !bytes.Equal(pkt, want) {
		t.Fatalf("packet header = % x, want % x", pkt[:18], want[:18])
	}
}

func TestDmxPacketUniverseAndLength(t *testing.T) {
	pkt := DmxPacket(0x0123, 9, []byte{1, 2, 3})
	if pkt[14] != 0x23 || pkt[15] != 0x01 {
		t.Errorf("SubUni/Net = %x %x", pkt[14], pkt[15])
	}
	if pkt[16] != 0 || pkt[17] != 4 || len(pkt) != 22 {
		t.Errorf("length not padded to even: %d bytes, len field %d", len(pkt), pkt[17])
	}
}

func TestParsePollReply(t *testing.T) {
	pkt := make([]byte, 239)
	copy(pkt, "Art-Net\x00")
	pkt[8], pkt[9] = 0x00, 0x21
	copy(pkt[10:], []byte{2, 0, 0, 1})
	copy(pkt[26:], "DMX-AN2")
	copy(pkt[44:], "Art-Net to DMX node")
	n, err := ParsePollReply(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if n.IP != "2.0.0.1" || n.ShortName != "DMX-AN2" || n.LongName != "Art-Net to DMX node" {
		t.Errorf("got %+v", n)
	}
	if _, err := ParsePollReply(PollPacket()); err == nil {
		t.Error("ArtPoll parsed as a reply")
	}
}

func TestRouteAndBroadcast(t *testing.T) {
	_, n, _ := net.ParseCIDR("2.0.0.2/8")
	n.IP = net.ParseIP("2.0.0.2").To4()
	_, other, _ := net.ParseCIDR("192.168.1.10/24")
	locals := []*net.IPNet{other, n}
	if got := RouteFor("2.0.0.1", locals); got != n {
		t.Fatalf("RouteFor = %v", got)
	}
	if RouteFor("10.0.0.1", locals) != nil {
		t.Error("unexpected route for 10.0.0.1")
	}
	if b := Broadcast(n); b != "2.255.255.255" {
		t.Errorf("broadcast = %s", b)
	}
}

func TestLoopbackSend(t *testing.T) {
	c, err := Listen()
	if err != nil {
		t.Skip("no UDP:", err)
	}
	defer c.Close()
	if err := c.SendDmx("127.0.0.1", 0, make([]byte, 512)); err != nil {
		t.Fatal(err)
	}
	if err := c.SendDmx("not-an-ip", 0, nil); err == nil {
		t.Error("expected error for bad IP")
	}
}
