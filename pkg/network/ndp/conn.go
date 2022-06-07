package ndp

import (
	"fmt"
	"net"
	"time"

	"github.com/mdlayher/ndp"
	"golang.org/x/net/ipv6"
)

const (
	chkOff              = 2
	exportSocketTimeout = 2 * time.Minute
	importSocketTimeout = time.Minute
	maxHops             = 255
	raBufferSize        = 128
	unixLocalNetwork    = "unix"
)

// A NDPConnection instruments a system.Conn and adds retry functionality for
// receiving / sending NDP messages on a given interface.
type NDPConnection struct {
	iface      *net.Interface
	rawConn    *net.IPConn
	conn       *ipv6.PacketConn
	controlMsg *ipv6.ControlMessage
}

// Return an NDPConnection bound to the chosen interface.
func NewNDPConnection(ifaceName string) (*NDPConnection, error) {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("could not find interface %s: %v", ifaceName, err)
	}

	listenAddr := &net.IPAddr{
		IP:   net.IPv6interfacelocalallnodes,
		Zone: ifaceName,
	}
	icmpListener, err := net.ListenIP("ip6:ipv6-icmp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("could not listen to ip6:ipv6-icmp on addr %s: %v", listenAddr.String(), err)
	}

	ipv6Conn := ipv6.NewPacketConn(icmpListener)

	// Calculate and place ICMPv6 checksum at correct offset in all messages.
	if err := ipv6Conn.SetChecksum(true, chkOff); err != nil {
		return nil, fmt.Errorf("could not enable ICMPv6 checksum processing: %v", err)
	}

	routersMulticastGroup := &net.IPAddr{
		IP:   net.IPv6linklocalallrouters,
		Zone: ifaceName,
	}
	if err := ipv6Conn.JoinGroup(iface, routersMulticastGroup); err != nil {
		return nil, fmt.Errorf("failed to join %s multicast group: %v", routersMulticastGroup.String(), err)
	}

	listener := &NDPConnection{
		iface:      iface,
		conn:       ipv6Conn,
		rawConn:    icmpListener,
		controlMsg: getIPv6ControlMsg(),
	}

	if err := listener.Filter(ipv6.ICMPTypeRouterSolicitation); err != nil {
		return nil, fmt.Errorf("failed to set an ICMP filter for RouterSolicitations")
	}

	return listener, nil
}

func getIPv6ControlMsg() *ipv6.ControlMessage {
	return &ipv6.ControlMessage{
		HopLimit: maxHops,
	}
}

func (l *NDPConnection) ReadFrom() (ndp.Message, error) {
	buf := make([]byte, raBufferSize)
	n, _, _, err := l.conn.ReadFrom(buf)
	if err != nil || n == 0 {
		return nil, fmt.Errorf("failed to read NDP. n bytes: %d, err: %v", n, err)
	}

	msg, err := ndp.ParseMessage(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshall NDP msg: %v", err)
	}
	return msg, err
}

func (l *NDPConnection) WriteTo(msg ndp.Message, dst net.IP) error {
	msgBytes, err := ndp.MarshalMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to marshall the NDP msg: %v", err)
	}
	dstAddr := &net.IPAddr{
		IP:   dst,
		Zone: l.iface.Name,
	}

	n, err := l.conn.WriteTo(msgBytes, l.controlMsg, dstAddr)
	if err != nil || n == 0 {
		return fmt.Errorf("failed to send the NDP msg to %s. Error: %v, n bytes: %d", dst.String(), err, n)
	}
	return nil
}

func (l *NDPConnection) Filter(icmpType ipv6.ICMPType) error {
	var filter ipv6.ICMPFilter
	filter.SetAll(true)
	filter.Accept(icmpType)

	if err := l.conn.SetICMPFilter(&filter); err != nil {
		return err
	}

	return nil
}
