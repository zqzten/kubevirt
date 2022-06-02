package dns

import (
	"bufio"
	"net"
	"regexp"
	"strings"
)

const (
	domainSearchPrefix  = "search"
	nameserverPrefix    = "nameserver"
	defaultIPv4DNS      = "8.8.8.8"
	defaultSearchDomain = "cluster.local"
)

var (
	reIPv4 = regexp.MustCompile("([0-9]{1,3}.?){4}")
	reIPv6 = regexp.MustCompile("([a-f0-9:]+:+)+[a-f0-9]+")
)

// returns IPv4 nameservers []net.IP, IPv6 nameservers []net.IP, error
func ParseNameservers(content string) ([]net.IP, []net.IP, error) {
	var ipv4Nameservers []net.IP
	var ipv6Nameservers []net.IP

	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, nameserverPrefix) {
			// try match IPv6 address first, due to the IPv4-mapped IPv6 address
			if nameserver := net.ParseIP(reIPv6.FindString(line)); nameserver != nil {
				ipv6Nameservers = append(ipv6Nameservers, nameserver)
				continue
			}
			if nameserver := net.ParseIP(reIPv4.FindString(line)); nameserver != nil {
				ipv4Nameservers = append(ipv4Nameservers, nameserver)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return ipv4Nameservers, ipv6Nameservers, err
	}

	// apply a default DNS if none found from pod
	if len(ipv4Nameservers) == 0 && len(ipv6Nameservers) == 0 {
		ipv4Nameservers = append(ipv4Nameservers, net.ParseIP(defaultIPv4DNS))
	}

	return ipv4Nameservers, ipv6Nameservers, nil
}

func ParseSearchDomains(content string) ([]string, error) {
	var searchDomains []string

	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, domainSearchPrefix) {
			doms := strings.Fields(strings.TrimPrefix(line, domainSearchPrefix))
			for _, dom := range doms {
				// domain names are case insensitive but kubernetes allows only lower-case
				searchDomains = append(searchDomains, strings.ToLower(dom))
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(searchDomains) == 0 {
		searchDomains = append(searchDomains, defaultSearchDomain)
	}

	return searchDomains, nil
}

//GetLongestServiceDomainName returns the longest service search domain entry
func GetLongestServiceDomainName(searchDomains []string) string {
	serviceDomains := GetServiceDomainList(searchDomains)
	return GetDomainName(serviceDomains)
}

//GetDomainName returns the longest search domain entry, which is the most exact equivalent to a domain
func GetDomainName(searchDomains []string) string {
	selected := ""
	for _, d := range searchDomains {
		if len(d) > len(selected) {
			selected = d
		}
	}
	return selected
}

//GetServiceDomainList returns a list of search domains which are a service entry
func GetServiceDomainList(searchDomains []string) []string {
	const k8sServiceInfix = ".svc."

	serviceDomains := []string{}
	for _, d := range searchDomains {
		if strings.Contains(d, k8sServiceInfix) {
			serviceDomains = append(serviceDomains, d)
		}
	}
	return serviceDomains
}

//DomainNameWithSubdomain returns the DNS domain according subdomain.
//In case subdomain already exists in the domain, returns empty string, as nothing should be added.
//In case subdomain is empty, returns empty string, as nothing should be added.
//The motivation is that glibc prior to 2.26 had 6 domain / 256 bytes limit,
//Due to this limitation subdomain.namespace.svc.cluster.local DNS was not added by k8s to the pod /etc/resolv.conf.
//This function calculates the missing domain, which will be added by kubevirt.
//see https://github.com/kubernetes/kubernetes/issues/48019 for more details.
func DomainNameWithSubdomain(searchDomains []string, subdomain string) string {
	if subdomain == "" {
		return ""
	}

	domainName := GetLongestServiceDomainName(searchDomains)
	if domainName != "" && !strings.HasPrefix(domainName, subdomain+".") {
		return subdomain + "." + domainName
	}

	return ""
}
