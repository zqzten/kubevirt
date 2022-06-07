package ndp

import (
	"fmt"
	"net"
	"time"

	"github.com/mdlayher/ndp"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"
)

const (
	routerAdvertisementMaxLifetime = 65535 * time.Second // check RFC 4861, section 4.2; 16 bit integer.
	routerAdvertisementPeriod      = 1 * time.Minute
)

type RouterAdvertiser struct {
	raOptions []ndp.Option
	ndpConn   *NDPConnection
}

func CreateRouterAdvertisementServer(ifaceName string, ipv6CIDR string, routerMACAddr net.HardwareAddr, dhcpOptions *v1.DHCPOptions) (*RouterAdvertiser, error) {
	ndpConnection, err := NewNDPConnection(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create NDP connection: %v", err)
	}

	prefix, network, err := net.ParseCIDR(ipv6CIDR)
	if err != nil {
		return nil, fmt.Errorf("could not compute prefix / prefix length from %s. Reason: %v", ipv6CIDR, err)
	}
	prefixLength, _ := network.Mask.Size()
	rad := &RouterAdvertiser{
		ndpConn:   ndpConnection,
		raOptions: prepareRAOptions(prefix, uint8(prefixLength), routerMACAddr, dhcpOptions),
	}
	return rad, nil
}

func (rad *RouterAdvertiser) Serve() error {
	for {
		msg, err := rad.ndpConn.ReadFrom()
		if err != nil {
			return err
		}

		switch msg.(type) {
		case *ndp.RouterSolicitation:
			log.Log.V(4).Info("Received RouterSolicitation msg. Will reply w/ RA")
			err = rad.SendRouterAdvertisement()
			if err != nil {
				return err
			}
		}
	}
}

func (rad *RouterAdvertiser) SendRouterAdvertisement() error {
	ra := &ndp.RouterAdvertisement{
		ManagedConfiguration: true,
		OtherConfiguration:   true,
		RouterLifetime:       routerAdvertisementMaxLifetime,
		ReachableTime:        ndp.Infinity,
		Options:              rad.raOptions,
	}

	if err := rad.ndpConn.WriteTo(ra, net.IPv6linklocalallnodes); err != nil {
		return fmt.Errorf("failed to send router advertisement: %v", err)
	}
	return nil
}

func (rad *RouterAdvertiser) PeriodicallySendRAs() {
	ticker := time.NewTicker(routerAdvertisementPeriod)

	for {
		select {
		case <-ticker.C:
			if err := rad.SendRouterAdvertisement(); err != nil {
				log.Log.Warningf("failed to send periodic RouterAdvertisement: %v", err)
			}
		}
	}
}

func prepareRAOptions(prefix net.IP, prefixLength uint8, routerMACAddr net.HardwareAddr, dhcpOptions *v1.DHCPOptions) []ndp.Option {
	prefixInfo := &ndp.PrefixInformation{
		PrefixLength:                   prefixLength,
		OnLink:                         true,
		AutonomousAddressConfiguration: false,
		ValidLifetime:                  ndp.Infinity,
		PreferredLifetime:              ndp.Infinity,
		Prefix:                         prefix,
	}

	sourceLinkLayerAddr := &ndp.LinkLayerAddress{
		Addr:      routerMACAddr,
		Direction: ndp.Source,
	}

	return []ndp.Option{prefixInfo, sourceLinkLayerAddr}
}
