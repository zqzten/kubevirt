/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2020 Red Hat, Inc.
 *
 */

package serverv6

import (
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/server6"
	"github.com/insomniacslk/dhcp/iana"

	"kubevirt.io/client-go/log"
)

const (
	infiniteLease = 999 * 24 * time.Hour
)

type DHCPv6Handler struct {
	serverMacAddr net.HardwareAddr
	clientIP      net.IP
	dnsIPs        []net.IP
	searchDomains []string
}

func SingleClientDHCPv6Server(clientIP net.IP, serverIfaceName string, dnsIPs []net.IP, searchDomains []string) error {
	log.Log.Info("Starting SingleClientDHCPv6Server")

	iface, err := net.InterfaceByName(serverIfaceName)
	if err != nil {
		return fmt.Errorf("couldn't create DHCPv6 server, couldn't get the dhcp6 server interface: %v", err)
	}

	handler := &DHCPv6Handler{
		serverMacAddr: iface.HardwareAddr,
		clientIP:      clientIP,
		dnsIPs:        dnsIPs,
		searchDomains: searchDomains,
	}

	conn, err := NewConnection(iface)
	if err != nil {
		return fmt.Errorf("couldn't create DHCPv6 server: %v", err)
	}

	s, err := server6.NewServer("", nil, handler.ServeDHCPv6, server6.WithConn(conn))
	if err != nil {
		return fmt.Errorf("couldn't create DHCPv6 server: %v", err)
	}

	err = s.Serve()
	if err != nil {
		return fmt.Errorf("failed to run DHCPv6 server: %v", err)
	}

	return nil
}

func (h *DHCPv6Handler) ServeDHCPv6(conn net.PacketConn, peer net.Addr, m dhcpv6.DHCPv6) {
	log.Log.V(4).Info("DHCPv6 serving a new request")

	// TODO if we extend the server to support bridge binding, we need to filter out non-vm requests

	response, err := h.buildResponse(m)
	if err != nil {
		log.Log.V(4).Reason(err).Error("DHCPv6 failed building a response to the client")
		return
	}

	if _, err := conn.WriteTo(response.ToBytes(), peer); err != nil {
		log.Log.V(4).Reason(err).Error("DHCPv6 failed sending a response to the client")
	}
}

func (h *DHCPv6Handler) buildResponse(msg dhcpv6.DHCPv6) (*dhcpv6.Message, error) {
	dhcpv6Msg := msg.(*dhcpv6.Message)
	switch dhcpv6Msg.Type() {
	case dhcpv6.MessageTypeSolicit:
		log.Log.V(4).Info("DHCPv6 - the request has message type Solicit")
		if dhcpv6Msg.GetOneOption(dhcpv6.OptionRapidCommit) != nil {
			log.Log.V(4).Info("DHCPv6 - replying with rapid commit")
			return dhcpv6.NewReplyFromMessage(dhcpv6Msg, []dhcpv6.Modifier{
				buildDUIDModifier(h.serverMacAddr),
				buildIAIDModifier(dhcpv6Msg),
				buildIAAddressModifier(h.clientIP),
				dhcpv6.WithDNS(h.dnsIPs...),
				dhcpv6.WithDomainSearchList(h.searchDomains...),
			}...)
		} else {
			log.Log.V(4).Info("DHCPv6 - replying with advertise")
			return dhcpv6.NewAdvertiseFromSolicit(dhcpv6Msg, []dhcpv6.Modifier{
				buildDUIDModifier(h.serverMacAddr),
				buildIAIDModifier(dhcpv6Msg),
				buildIAAddressModifier(h.clientIP),
			}...)
		}
	case dhcpv6.MessageTypeRequest:
		log.Log.V(4).Info("DHCPv6 - the request has message type Request")
		log.Log.V(4).Info("DHCPv6 - replying with reply")
		return dhcpv6.NewReplyFromMessage(dhcpv6Msg, []dhcpv6.Modifier{
			buildDUIDModifier(h.serverMacAddr),
			buildIAIDModifier(dhcpv6Msg),
			buildIAAddressModifier(h.clientIP),
			dhcpv6.WithDNS(h.dnsIPs...),
			dhcpv6.WithDomainSearchList(h.searchDomains...),
		}...)
	default:
		log.Log.V(4).Infof("DHCPv6 - %s request received", dhcpv6Msg.Type().String())
		return dhcpv6.NewReplyFromMessage(dhcpv6Msg, buildDUIDModifier(h.serverMacAddr))
	}
}

func buildIAIDModifier(recvMsg *dhcpv6.Message) dhcpv6.Modifier {
	return func(d dhcpv6.DHCPv6) {
		ianaRequest := recvMsg.Options.OneIANA()
		if ianaRequest == nil || len(ianaRequest.IaId) == 0 {
			log.Log.V(4).Errorf("DHCPv6 - recv message does not contain IAID")
			return
		}
		dhcpv6.WithIAID(ianaRequest.IaId)(d)
	}
}

func buildDUIDModifier(serverInterfaceMac net.HardwareAddr) dhcpv6.Modifier {
	duid := dhcpv6.Duid{Type: dhcpv6.DUID_LL, HwType: iana.HWTypeEthernet, LinkLayerAddr: serverInterfaceMac}
	return dhcpv6.WithServerID(duid)
}

func buildIAAddressModifier(clientIP net.IP) dhcpv6.Modifier {
	optIAAddress := dhcpv6.OptIAAddress{IPv6Addr: clientIP, PreferredLifetime: infiniteLease, ValidLifetime: infiniteLease}
	return dhcpv6.WithIANA(optIAAddress)
}
