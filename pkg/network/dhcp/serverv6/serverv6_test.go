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
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/iana"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("DHCPv6", func() {
	Context("buildResponse should build a response with", func() {
		var handler *DHCPv6Handler

		BeforeEach(func() {
			clientIP := net.ParseIP("fd10:0:2::2")
			serverInterfaceMac, _ := net.ParseMAC("12:34:56:78:9A:BC")
			dnsIPs := []net.IP{net.ParseIP("2001:4860:4860::8888")}
			searchDomains := []string{"svc.cluster.local"}

			handler = &DHCPv6Handler{
				serverMacAddr: serverInterfaceMac,
				clientIP:      clientIP,
				dnsIPs:        dnsIPs,
				searchDomains: searchDomains,
			}
		})

		It("advertise type on rapid commit solicit request", func() {
			clientMessage, err := newMessage(dhcpv6.MessageTypeSolicit)
			Expect(err).ToNot(HaveOccurred())
			dhcpv6.WithRapidCommit(clientMessage)

			replyMessage, err := handler.buildResponse(clientMessage)
			Expect(err).ToNot(HaveOccurred())
			Expect(replyMessage.Type()).To(Equal(dhcpv6.MessageTypeReply))
		})
		It("reply type on solicit request", func() {
			clientMessage, err := newMessage(dhcpv6.MessageTypeSolicit)
			Expect(err).ToNot(HaveOccurred())

			replyMessage, err := handler.buildResponse(clientMessage)
			Expect(err).ToNot(HaveOccurred())
			Expect(replyMessage.Type()).To(Equal(dhcpv6.MessageTypeAdvertise))
		})
		It("reply type on request request", func() {
			clientMessage, err := newMessage(dhcpv6.MessageTypeRequest)
			Expect(err).ToNot(HaveOccurred())

			replyMessage, err := handler.buildResponse(clientMessage)
			Expect(err).ToNot(HaveOccurred())
			Expect(replyMessage.Type()).To(Equal(dhcpv6.MessageTypeReply))
		})
		It("reply type on other request", func() {
			clientMessage, err := newMessage(dhcpv6.MessageTypeInformationRequest)
			Expect(err).ToNot(HaveOccurred())

			replyMessage, err := handler.buildResponse(clientMessage)
			Expect(err).ToNot(HaveOccurred())
			Expect(replyMessage.Type()).To(Equal(dhcpv6.MessageTypeReply))
		})
		It("iana option containing the iaid from the request", func() {
			clientMessage, err := newMessage(dhcpv6.MessageTypeSolicit)
			iaId := [4]byte{5, 6, 7, 8}
			clientMessage.UpdateOption(&dhcpv6.OptIANA{IaId: iaId})
			Expect(err).ToNot(HaveOccurred())

			replyMessage, err := handler.buildResponse(clientMessage)
			Expect(err).ToNot(HaveOccurred())
			Expect(replyMessage.Options.OneIANA().IaId).To(Equal([4]byte{5, 6, 7, 8}))
		})
		It("reply response should contain dns & searchDomain", func() {
			clientMessage, err := newMessage(dhcpv6.MessageTypeRequest)
			iaId := [4]byte{5, 6, 7, 8}
			clientMessage.UpdateOption(&dhcpv6.OptIANA{IaId: iaId})
			Expect(err).ToNot(HaveOccurred())

			replyMessage, err := handler.buildResponse(clientMessage)
			Expect(err).ToNot(HaveOccurred())
			Expect(replyMessage.Type()).To(Equal(dhcpv6.MessageTypeReply))
			Expect(replyMessage.Options.OneIANA().IaId).To(Equal([4]byte{5, 6, 7, 8}))
			Expect(replyMessage.Options.DNS()).To(Equal([]net.IP{net.ParseIP("2001:4860:4860::8888")}))
			Expect(replyMessage.Options.DomainSearchList().Labels).To(Equal([]string{"svc.cluster.local"}))
		})
	})
})

func newMessage(messageType dhcpv6.MessageType) (*dhcpv6.Message, error) {
	clientMac, _ := net.ParseMAC("34:56:78:9A:BC:DE")
	duid := dhcpv6.Duid{Type: dhcpv6.DUID_LL, HwType: iana.HWTypeEthernet, LinkLayerAddr: clientMac}
	clientMessage, err := dhcpv6.NewMessage(dhcpv6.WithIAID([4]byte{1, 2, 3, 4}), dhcpv6.WithClientID(duid))
	clientMessage.MessageType = messageType
	return clientMessage, err
}
