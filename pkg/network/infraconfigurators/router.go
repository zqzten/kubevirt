package infraconfigurators

import (
	"fmt"
	"net"
	"strconv"

	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/pkg/network/cache"
	netdriver "kubevirt.io/kubevirt/pkg/network/driver"
	virtnetlink "kubevirt.io/kubevirt/pkg/network/link"
	"kubevirt.io/kubevirt/pkg/util"
	"kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/api"
)

var (
	_, localIPv4Addr, _  = net.ParseCIDR("169.254.75.1/32")
	_, localIPv6Addr, _  = net.ParseCIDR("fc00:eeee:eeee:eeee:eeee:eeee:eeee:eeee/128")
	gatewayMacAddress, _ = net.ParseMAC("ee:ee:ee:ee:ee:ee")
)

type RouterPodNetworkConfigurator struct {
	handler            netdriver.NetworkHandler
	vmi                *v1.VirtualMachineInstance
	vmiSpecIface       *v1.Interface
	ipv4Enabled        bool
	ipv6Enabled        bool
	launcherPID        int
	vmMac              *net.HardwareAddr
	podNicLink         netlink.Link
	tapDeviceName      string
	tapNicLink         netlink.Link
	bridgeDeviceName   string
	bridgeLink         netlink.Link
	podIfaceIPv4Routes []netlink.Route
	podIPv4Addr        netlink.Addr
	podIfaceIPv6Routes []netlink.Route
	podIPv6Addr        netlink.Addr
	defaultIPv4Gateway net.IP
	defaultIPv6Gateway net.IP
}

// router network mode
// VM IP = POD IP;
// export reserved ports in POD net ns, which can be accessed through POD IP;
// VM can export ports except reserved ports;
func NewRouterPodNetworkConfigurator(vmi *v1.VirtualMachineInstance, vmiSpecIface *v1.Interface, launcherPID int, handler netdriver.NetworkHandler) *RouterPodNetworkConfigurator {
	return &RouterPodNetworkConfigurator{
		vmi:          vmi,
		vmiSpecIface: vmiSpecIface,
		launcherPID:  launcherPID,
		handler:      handler,
	}
}

func (b *RouterPodNetworkConfigurator) DiscoverPodNetworkInterface(podIfaceName string) error {
	if link, err := b.handler.LinkByName(podIfaceName); err != nil {
		log.Log.Reason(err).Errorf("failed to get a link for interface: %s", podIfaceName)
		return err
	} else {
		b.podNicLink = link
	}

	if ipv4, ipv6, err := b.handler.ReadIPv4AndIPv6AddrFromLink(podIfaceName); err != nil {
		log.Log.Reason(err).Errorf("failed to get v4 or v6 addr for interface: %s", podIfaceName)
		return err
	} else {
		if ipv4 != nil {
			b.ipv4Enabled = true
			b.podIPv4Addr = *ipv4
		}
		if ipv6 != nil {
			b.ipv6Enabled = true
			b.podIPv6Addr = *ipv6
		}
	}

	if b.ipv6Enabled {
		if err := b.handler.EnableIPv6Flags(); err != nil {
			return err
		}
	}

	if err := b.learnInterfaceRoutes(); err != nil {
		return err
	}

	b.tapDeviceName = virtnetlink.GenerateTapDeviceName(podIfaceName)
	b.bridgeDeviceName = virtnetlink.GenerateInPodBridgeInterfaceName(podIfaceName)

	if macAddr, err := virtnetlink.RetrieveMacAddressFromVMISpecIface(b.vmiSpecIface); err != nil {
		return err
	} else if macAddr != nil {
		b.vmMac = macAddr
	} else {
		b.vmMac = &b.podNicLink.Attrs().HardwareAddr
	}

	return nil
}

// 1. configure POD interface eth0
// 	 1.1 remove Pod IP from eth0
//   1.2 change eth0 MAC address（assign to VM net dev）
// 2. create tapDev
// 3. configure bridgeDev
//   3.1 bind tapDev to bridgeDev
//   3.2 add local IP to bridgeDev
// 4. configure route and DNAT rule
//	 4.1 DNAT to local IP for reserved ports
//   4.2 route to bridgeDev for POD IP
// 5. add neighbour proxy for POD IP
//   5.1 proxy POD IP on POD interface
// 6. if IPv6, start NDP server in POD net ns
func (b *RouterPodNetworkConfigurator) PreparePodNetworkInterface() error {
	if err := b.configurePodNic(); err != nil {
		log.Log.Reason(err).Errorf("failed to configure POD interface: %s", b.podNicLink.Attrs().Name)
		return err
	}

	if err := b.createTapDevice(); err != nil {
		log.Log.Reason(err).Errorf("failed to configure tap: %s", b.tapDeviceName)
		return err
	}

	if err := b.createBridge(); err != nil {
		log.Log.Reason(err).Errorf("failed to configure bridge: %s", b.bridgeDeviceName)
		return err
	}

	reservedPorts := reservedPortsInPod(b.vmi)
	if b.ipv4Enabled {
		podIPNet := &net.IPNet{
			IP:   b.podIPv4Addr.IP,
			Mask: net.CIDRMask(8*net.IPv4len, 8*net.IPv4len),
		}
		if err := b.createNATRulesForReservedPorts(iptables.ProtocolIPv4, reservedPorts, localIPv4Addr, podIPNet, netdriver.DefaultIPv4Dst, b.defaultIPv4Gateway, gatewayMacAddress); err != nil {
			log.Log.Reason(err).Errorf("failed to create NAT rules for ipv4")
			return err
		}
	}

	if b.ipv6Enabled {
		podIPNet := &net.IPNet{
			IP:   b.podIPv6Addr.IP,
			Mask: net.CIDRMask(8*net.IPv6len, 8*net.IPv6len),
		}
		if err := b.createNATRulesForReservedPorts(iptables.ProtocolIPv6, reservedPorts, localIPv6Addr, podIPNet, netdriver.DefaultIPv6Dst, b.defaultIPv6Gateway, gatewayMacAddress); err != nil {
			log.Log.Reason(err).Errorf("failed to create NAT rules for ipv6")
			return err
		}
	}

	if err := b.configureNeighbourReply(); err != nil {
		log.Log.Reason(err).Errorf("failed to configure neighbour proxy")
		return err
	}

	return nil
}

func (b *RouterPodNetworkConfigurator) GenerateNonRecoverableDomainIfaceSpec() *api.Interface {
	return &api.Interface{
		MAC: &api.MAC{MAC: b.vmMac.String()},
	}
}

func (b *RouterPodNetworkConfigurator) GenerateNonRecoverableDHCPConfig() *cache.DHCPConfig {
	if !b.ipv4Enabled && !b.ipv6Enabled {
		return &cache.DHCPConfig{IPAMDisabled: true}
	}

	dhcpConfig := &cache.DHCPConfig{
		MAC:          *b.vmMac,
		IPAMDisabled: false,
	}

	if b.ipv4Enabled {
		log.Log.V(4).Infof("got to add %d ipv4 routes to the DhcpConfig", len(b.podIfaceIPv4Routes))
		ipv4Addr, err := netlink.ParseAddr(fmt.Sprintf("%s/%d", b.podIPv4Addr.IP.String(), 8*net.IPv4len))
		if err != nil {
			log.Log.Errorf("failed to parse IPv4 addr(%s): %v", b.podIPv4Addr.String(), err)
		}
		dhcpConfig.IP = *ipv4Addr
		if len(b.podIfaceIPv4Routes) > 1 {
			dhcpRoutes := virtnetlink.FilterPodNetworkRoutes(b.podIfaceIPv4Routes, dhcpConfig)
			dhcpConfig.Routes = &dhcpRoutes
		}
		dhcpConfig.Gateway = localIPv4Addr.IP
	}

	if b.ipv6Enabled {
		log.Log.V(4).Infof("got to add %d ipv6 routes to the DhcpConfig", len(b.podIfaceIPv6Routes))
		ipv6Addr, err := netlink.ParseAddr(fmt.Sprintf("%s/%d", b.podIPv6Addr.IP.String(), 8*net.IPv6len))
		if err != nil {
			log.Log.Errorf("failed to parse IPv6 addr(%s): %v", b.podIPv6Addr.String(), err)
		}
		dhcpConfig.IPv6 = *ipv6Addr
		if len(b.podIfaceIPv6Routes) > 1 {
			dhcpRoutes := virtnetlink.FilterPodNetworkRoutes(b.podIfaceIPv6Routes, dhcpConfig)
			dhcpConfig.IPv6Routes = &dhcpRoutes
		}
		dhcpConfig.IPv6Gateway = b.defaultIPv6Gateway
	}
	return dhcpConfig
}

func (b *RouterPodNetworkConfigurator) learnInterfaceRoutes() error {
	if b.ipv4Enabled {
		routes, err := b.handler.RouteList(b.podNicLink, netlink.FAMILY_V4)
		if err != nil {
			log.Log.Reason(err).Errorf("failed to get routes for %s", b.podNicLink.Attrs().Name)
			return err
		}
		if len(routes) == 0 {
			return fmt.Errorf("no gateway address found in routes for %s", b.podNicLink.Attrs().Name)
		}
		b.podIfaceIPv4Routes = routes
		if gw, err := b.handler.GetDefaultGateway(netlink.FAMILY_V4); err != nil {
			return fmt.Errorf("failed to get v4 gateway: %v", err)
		} else {
			b.defaultIPv4Gateway = gw
		}
	}
	if b.ipv6Enabled {
		routes, err := b.handler.RouteList(b.podNicLink, netlink.FAMILY_V6)
		if err != nil {
			log.Log.Reason(err).Errorf("failed to get routes for %s", b.podNicLink.Attrs().Name)
			return err
		}
		if len(routes) == 0 {
			return fmt.Errorf("no gateway address found in routes for %s", b.podNicLink.Attrs().Name)
		}
		b.podIfaceIPv6Routes = routes
		if gw, err := b.handler.GetDefaultGateway(netlink.FAMILY_V6); err != nil {
			return fmt.Errorf("failed to get v6 gateway: %v", err)
		} else {
			b.defaultIPv6Gateway = gw
		}
	}
	return nil
}

func (b *RouterPodNetworkConfigurator) configurePodNic() error {
	if err := b.handler.LinkSetDown(b.podNicLink); err != nil {
		log.Log.Reason(err).Errorf("failed to bring link down for interface: %s", b.podNicLink.Attrs().Name)
		return err
	}

	if b.ipv4Enabled || b.ipv6Enabled {
		if b.ipv4Enabled {
			_ = b.handler.AddrDel(b.podNicLink, &b.podIPv4Addr)
		}

		if b.ipv6Enabled {
			_ = b.handler.AddrDel(b.podNicLink, &b.podIPv6Addr)
		}
	}

	if _, err := b.handler.SetRandomMac(b.podNicLink.Attrs().Name); err != nil {
		return err
	}

	if err := b.handler.LinkSetUp(b.podNicLink); err != nil {
		log.Log.Reason(err).Errorf("failed to bring link up for interface: %s", b.podNicLink.Attrs().Name)
		return err
	}

	return nil
}

func (b *RouterPodNetworkConfigurator) createTapDevice() error {
	tapOwner := netdriver.LibvirtUserAndGroupId
	if util.IsNonRootVMI(b.vmi) {
		tapOwner = strconv.Itoa(util.NonRootUID)
	}

	err := b.handler.CreateTapDevice(b.tapDeviceName, calculateNetworkQueues(b.vmi), b.launcherPID, b.podNicLink.Attrs().MTU, tapOwner)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to create tap device named %s", b.tapDeviceName)
		return err
	}

	b.tapNicLink, err = b.handler.LinkByName(b.tapDeviceName)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to get a link for interface: %s", b.tapDeviceName)
		return err
	}

	if err := b.handler.LinkSetUp(b.tapNicLink); err != nil {
		log.Log.Reason(err).Errorf("failed to bring link up for interface: %s", b.tapDeviceName)
		return err
	}

	return nil
}

func (b *RouterPodNetworkConfigurator) createBridge() error {
	bridge := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: b.bridgeDeviceName,
		},
	}
	err := b.handler.LinkAdd(bridge)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to create a bridge")
		return err
	}

	err = b.handler.LinkSetMaster(b.tapNicLink, bridge)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to connect interface %s to bridge %s", b.tapNicLink.Attrs().Name, bridge.Name)
		return err
	}

	err = b.handler.LinkSetUp(bridge)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to bring link up for interface: %s", b.bridgeDeviceName)
		return err
	}

	if b.ipv4Enabled {
		addr, _ := b.handler.ParseAddr(localIPv4Addr.String())
		if err := b.handler.AddrAdd(bridge, addr); err != nil {
			log.Log.Reason(err).Errorf("failed to set local Nic v4 IP")
			return err
		}
	}

	if b.ipv6Enabled {
		addr, _ := b.handler.ParseAddr(localIPv6Addr.String())
		if err := b.handler.AddrAdd(bridge, addr); err != nil {
			log.Log.Reason(err).Errorf("failed to set fake Nic v6 IP")
			return err
		}
	}

	b.bridgeLink, err = b.handler.LinkByName(b.bridgeDeviceName)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to get a link for interface: %s", b.bridgeDeviceName)
		return err
	}

	return nil
}

func (b *RouterPodNetworkConfigurator) configureNeighbourReply() error {
	if b.ipv4Enabled {
		if err := b.handler.EnableIpv4ArpProxyOnIface(b.podNicLink.Attrs().Name); err != nil {
			return err
		}

		if err := b.handler.NeighAdd(&netlink.Neigh{
			LinkIndex: b.podNicLink.Attrs().Index,
			Family:    netlink.FAMILY_V4,
			Flags:     netlink.NTF_PROXY,
			IP:        b.podIPv4Addr.IP,
		}); err != nil {
			return err
		}
	}

	if b.ipv6Enabled {
		// MUST enable to answer unicast Neighbor Solicitation
		// https://github.com/lxc/lxd/issues/6668
		if err := b.handler.EnableIpv6NdpProxyForAll(); err != nil {
			return err
		}

		// MUST enable to answer multicast Neighbor Solicitation
		if err := b.handler.EnableIpv6NdpProxyOnIface(b.podNicLink.Attrs().Name); err != nil {
			return err
		}

		if err := b.handler.NeighAdd(&netlink.Neigh{
			LinkIndex: b.podNicLink.Attrs().Index,
			Family:    netlink.FAMILY_V6,
			Flags:     netlink.NTF_PROXY,
			IP:        b.podIPv6Addr.IP,
		}); err != nil {
			return err
		}
	}

	return nil
}

// we need to re-configure eth0 route and neigh, due to eth0 dev down
func (b *RouterPodNetworkConfigurator) createNATRulesForReservedPorts(proto iptables.Protocol, ports []v1.Port, localAddr *net.IPNet, podIP *net.IPNet, defaultDst *net.IPNet, defaultGateway net.IP, gatewayMacAddr net.HardwareAddr) error {
	err := b.handler.ConfigureIpForwarding(proto)
	if err != nil {
		log.Log.Reason(err).Errorf("failed to configure ip forwarding")
		return err
	}

	if b.handler.NftablesLoad(proto) == nil {
		log.Log.Infof("config NAT rules by nftables")
		for _, port := range ports {
			protoToken := "tcp"
			if port.Protocol == "UDP" {
				protoToken = "udp"
			}
			if err := b.handler.NftablesAppendRule(proto, "nat", "prerouting",
				"iifname", b.podNicLink.Attrs().Name,
				protoToken,
				"dport", fmt.Sprintf("%d", port.Port),
				"dnat", localAddr.IP.String(),
			); err != nil {
				log.Log.Reason(err).Errorf("failed to add DNAT nftables rule for reserved port: %+v", port)
				return err
			}
		}
	} else if b.handler.HasNatIptables(proto) {
		log.Log.Infof("config NAT rules by iptables")
		for _, port := range ports {
			protoToken := "tcp"
			if port.Protocol == "UDP" {
				protoToken = "udp"
			}
			if err := b.handler.IptablesAppendRule(proto, "nat", "PREROUTING",
				"-p", protoToken,
				"--dport", fmt.Sprintf("%d", port.Port),
				"--jump", "DNAT",
				"--to-destination", localAddr.IP.String(),
			); err != nil {
				log.Log.Reason(err).Errorf("failed to add DNAT iptables rule for reserved port: %+v", port)
				return err
			}
		}
	}

	familyID := netlink.FAMILY_V4
	if proto == iptables.ProtocolIPv6 {
		familyID = netlink.FAMILY_V6
	}

	if err := b.handler.RouteAdd(&netlink.Route{
		LinkIndex: b.podNicLink.Attrs().Index,
		Flags:     int(netlink.FLAG_ONLINK),
		Dst:       defaultDst,
		Gw:        defaultGateway,
	}); err != nil {
		log.Log.Reason(err).Errorf("failed to add default gateway")
		return err
	}

	if err := b.handler.NeighAdd(&netlink.Neigh{
		State:        netlink.NUD_PERMANENT,
		Family:       familyID,
		LinkIndex:    b.podNicLink.Attrs().Index,
		IP:           defaultGateway,
		HardwareAddr: gatewayMacAddr,
	}); err != nil {
		log.Log.Reason(err).Errorf("failed to configure gateway neigh")
		return err
	}

	if err := b.handler.RouteAdd(&netlink.Route{
		LinkIndex: b.bridgeLink.Attrs().Index,
		Scope:     netlink.SCOPE_HOST,
		Dst:       podIP,
	}); err != nil {
		log.Log.Reason(err).Errorf("failed to add route for POD IP on bridge: %s, %s", b.bridgeLink.Attrs().Name, podIP.String())
		return err
	}

	return nil
}
