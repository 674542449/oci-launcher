package oci

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

// OCI hard limit: 200 ingress rules per security list (cannot be raised).
const maxIngressRulesPerSecurityList = 200

// Cloudflare official IPv4 & IPv6 CIDR blocks (https://www.cloudflare.com/ips/)
var CloudflareCIDRs = []struct {
	CIDR string
	IsV6 bool
}{
	// IPv4
	{"173.245.48.0/20", false},
	{"103.21.244.0/22", false},
	{"103.22.200.0/22", false},
	{"103.31.4.0/22", false},
	{"141.101.64.0/18", false},
	{"108.162.192.0/18", false},
	{"190.93.240.0/20", false},
	{"188.114.96.0/20", false},
	{"197.234.240.0/22", false},
	{"198.41.128.0/17", false},
	{"162.158.0.0/15", false},
	{"104.16.0.0/13", false},
	{"104.24.0.0/14", false},
	{"172.64.0.0/13", false},
	{"131.0.72.0/22", false},
	// IPv6
	{"2400:cb00::/32", true},
	{"2606:4700::/32", true},
	{"2803:f800::/32", true},
	{"2405:b500::/32", true},
	{"2405:8100::/32", true},
	{"2a06:98c0::/29", true},
	{"2c0f:f248::/32", true},
}

type VCNItem struct {
	OCID        string `json:"ocid"`
	Compartment string `json:"compartment"` // "root" or the sub-compartment name
	DisplayName string `json:"display_name"`
	CidrBlock   string `json:"cidr_block"`
	Ipv6Enabled bool   `json:"ipv6_enabled"`
	State       string `json:"state"`
}

type SubnetItem struct {
	OCID           string `json:"ocid"`
	DisplayName    string `json:"display_name"`
	VcnID          string `json:"vcn_id"`
	CidrBlock      string `json:"cidr_block"`
	Ipv6CidrBlock  string `json:"ipv6_cidr_block"`
	SecurityListID string `json:"security_list_id"`
	State          string `json:"state"`
}

type SecurityRuleItem struct {
	Key         string `json:"key"` // identifies the rule for single-rule deletion
	Protocol    string `json:"protocol"`
	Source      string `json:"source"`
	Description string `json:"description"`
	PortRange   string `json:"port_range"`
	IsStateless bool   `json:"is_stateless"`
}

// IngressRuleSpec is a single user-defined ingress rule.
type IngressRuleSpec struct {
	Protocol    string // tcp, udp, icmp, all
	Source      string // CIDR (a bare IP is accepted and turned into /32 or /128)
	PortMin     int    // tcp/udp only; 0 = all ports
	PortMax     int
	Description string
	IsStateless bool
}

// NormalizeCIDR accepts "1.2.3.4", "1.2.3.0/24", "2001:db8::1" or "::/0" and returns canonical CIDR notation.
func NormalizeCIDR(s string) (string, bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false, fmt.Errorf("来源不能为空")
	}
	if !strings.Contains(s, "/") {
		ip := net.ParseIP(s)
		if ip == nil {
			return "", false, fmt.Errorf("来源 %q 不是合法的 IP 或 CIDR", s)
		}
		if ip.To4() != nil {
			return ip.String() + "/32", false, nil
		}
		return ip.String() + "/128", true, nil
	}
	ip, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		return "", false, fmt.Errorf("来源 %q 不是合法的 CIDR", s)
	}
	isV6 := ip.To4() == nil
	return ipNet.String(), isV6, nil
}

// BuildIngressRule validates a spec and turns it into an SDK rule.
func BuildIngressRule(spec IngressRuleSpec) (core.IngressSecurityRule, error) {
	source, isV6, err := NormalizeCIDR(spec.Source)
	if err != nil {
		return core.IngressSecurityRule{}, err
	}

	rule := core.IngressSecurityRule{
		Source:     common.String(source),
		SourceType: core.IngressSecurityRuleSourceTypeCidrBlock,
	}
	if d := strings.TrimSpace(spec.Description); d != "" {
		if len(d) > 255 {
			d = d[:255]
		}
		rule.Description = common.String(d)
	}
	if spec.IsStateless {
		rule.IsStateless = common.Bool(true)
	}

	var portRange *core.PortRange
	if spec.PortMin > 0 || spec.PortMax > 0 {
		lo, hi := spec.PortMin, spec.PortMax
		if hi == 0 {
			hi = lo
		}
		if lo == 0 {
			lo = hi
		}
		if lo < 1 || hi > 65535 || lo > hi {
			return core.IngressSecurityRule{}, fmt.Errorf("端口范围无效: %d-%d（1-65535，且起始不大于结束）", lo, hi)
		}
		portRange = &core.PortRange{Min: common.Int(lo), Max: common.Int(hi)}
	}

	switch strings.ToLower(strings.TrimSpace(spec.Protocol)) {
	case "tcp", "6":
		rule.Protocol = common.String("6")
		if portRange != nil {
			rule.TcpOptions = &core.TcpOptions{DestinationPortRange: portRange}
		}
	case "udp", "17":
		rule.Protocol = common.String("17")
		if portRange != nil {
			rule.UdpOptions = &core.UdpOptions{DestinationPortRange: portRange}
		}
	case "icmp", "1", "58", "icmpv6":
		if isV6 {
			rule.Protocol = common.String("58") // ICMPv6 for IPv6 sources
		} else {
			rule.Protocol = common.String("1")
		}
	case "all", "":
		rule.Protocol = common.String("all")
	default:
		return core.IngressSecurityRule{}, fmt.Errorf("协议 %q 不支持，可选 tcp / udp / icmp / all", spec.Protocol)
	}
	return rule, nil
}

// AddSecurityRule adds one ingress rule to a security list (no-op if an identical rule exists).
func AddSecurityRule(ctx context.Context, profile *storage.OCIProfile, region, secListID string, spec IngressRuleSpec) (added bool, err error) {
	rule, err := BuildIngressRule(spec)
	if err != nil {
		return false, err
	}
	n, err := applyIngressRules(ctx, profile, region, secListID, []core.IngressSecurityRule{rule})
	return n > 0, err
}

// DeleteSecurityRule removes the ingress rule identified by key (from ListSecurityRules).
func DeleteSecurityRule(ctx context.Context, profile *storage.OCIProfile, region, secListID, key string) (removed int, err error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return 0, err
	}
	getResp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return 0, err
	}

	kept := make([]core.IngressSecurityRule, 0, len(getResp.SecurityList.IngressSecurityRules))
	for _, r := range getResp.SecurityList.IngressSecurityRules {
		if ruleKey(r) == key {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed == 0 {
		return 0, fmt.Errorf("规则不存在或已被删除，请刷新列表")
	}

	_, err = netClient.UpdateSecurityList(ctx, core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: kept,
		},
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

// ListVCNs lists VCNs in every compartment of the tenancy (all pages)
func ListVCNs(ctx context.Context, profile *storage.OCIProfile, region string) ([]VCNItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	var items []VCNItem
comps:
	for _, comp := range ListCompartments(ctx, profile) {
		req := core.ListVcnsRequest{
			CompartmentId: common.String(comp.ID),
			Limit:         common.Int(100),
		}
		for {
			resp, err := netClient.ListVcns(ctx, req)
			if err != nil {
				if skipUnreadableCompartment(comp, profile, err) {
					continue comps
				}
				return nil, err
			}
			for _, vcn := range resp.Items {
				if vcn.LifecycleState == core.VcnLifecycleStateTerminated || vcn.LifecycleState == core.VcnLifecycleStateTerminating {
					continue
				}
				items = append(items, VCNItem{
					Compartment: comp.Name,
					OCID:        StrVal(vcn.Id),
					DisplayName: StrVal(vcn.DisplayName),
					CidrBlock:   StrVal(vcn.CidrBlock),
					Ipv6Enabled: len(vcn.Ipv6CidrBlocks) > 0,
					State:       string(vcn.LifecycleState),
				})
			}
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
	}

	return items, nil
}

// ListSubnets lists subnets for a VCN (all pages, all compartments)
func ListSubnets(ctx context.Context, profile *storage.OCIProfile, region, vcnID string) ([]SubnetItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	var items []SubnetItem
comps:
	for _, comp := range ListCompartments(ctx, profile) {
		req := core.ListSubnetsRequest{
			CompartmentId: common.String(comp.ID),
			VcnId:         common.String(vcnID),
			Limit:         common.Int(100),
		}
		for {
			resp, err := netClient.ListSubnets(ctx, req)
			if err != nil {
				if skipUnreadableCompartment(comp, profile, err) {
					continue comps
				}
				return nil, err
			}
			for _, sub := range resp.Items {
				if sub.LifecycleState == core.SubnetLifecycleStateTerminated || sub.LifecycleState == core.SubnetLifecycleStateTerminating {
					continue
				}
				secListID := ""
				if len(sub.SecurityListIds) > 0 {
					secListID = sub.SecurityListIds[0]
				}
				ipv6 := StrVal(sub.Ipv6CidrBlock)
				if ipv6 == "" && len(sub.Ipv6CidrBlocks) > 0 {
					ipv6 = sub.Ipv6CidrBlocks[0]
				}
				items = append(items, SubnetItem{
					OCID:           StrVal(sub.Id),
					DisplayName:    StrVal(sub.DisplayName),
					VcnID:          StrVal(sub.VcnId),
					CidrBlock:      StrVal(sub.CidrBlock),
					Ipv6CidrBlock:  ipv6,
					SecurityListID: secListID,
					State:          string(sub.LifecycleState),
				})
			}
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
	}

	return items, nil
}

// firstIPv6Subnet derives the first /64 out of the VCN's /56 prefix.
func firstIPv6Subnet(vcnCidr string) string {
	_, ipNet, err := net.ParseCIDR(vcnCidr)
	if err != nil || ipNet.IP.To4() != nil {
		return ""
	}
	ones, _ := ipNet.Mask.Size()
	if ones > 64 {
		return ""
	}
	return fmt.Sprintf("%s/64", ipNet.IP.String())
}

// waitForVcnAvailable polls the VCN until it is AVAILABLE (or the timeout elapses).
func waitForVcnAvailable(ctx context.Context, netClient core.VirtualNetworkClient, vcnID string, timeout time.Duration) (core.Vcn, error) {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := netClient.GetVcn(ctx, core.GetVcnRequest{VcnId: common.String(vcnID)})
		if err != nil {
			return core.Vcn{}, err
		}
		if resp.Vcn.LifecycleState == core.VcnLifecycleStateAvailable {
			return resp.Vcn, nil
		}
		if time.Now().After(deadline) {
			return resp.Vcn, nil
		}
		select {
		case <-ctx.Done():
			return resp.Vcn, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func tcpRule(source, desc string, port int) core.IngressSecurityRule {
	rule := core.IngressSecurityRule{
		Protocol:   common.String("6"),
		Source:     common.String(source),
		SourceType: core.IngressSecurityRuleSourceTypeCidrBlock,
		TcpOptions: &core.TcpOptions{
			DestinationPortRange: &core.PortRange{Min: common.Int(port), Max: common.Int(port)},
		},
	}
	if desc != "" {
		rule.Description = common.String(desc)
	}
	return rule
}

// CreateRecommendedVCN creates what the console's "Create VCN" flow creates: an IPv6-enabled
// VCN, an Internet Gateway, default routes and one public subnet, with the default security
// list left as OCI creates it (SSH 22 and ICMP). Extra ports and inbound IPv6 are opened
// from the firewall page. Non-fatal problems (e.g. no IPv6 in the region) come back as warnings.
func CreateRecommendedVCN(ctx context.Context, profile *storage.OCIProfile, region string) (vcnID, subnetID string, warnings []string, err error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", "", nil, err
	}

	// Names and DNS labels follow the OCI console's own convention (vcn-YYYYMMDD-HHMM and friends)
	stamp := NameStamp()
	vcnName := "vcn-" + stamp
	igwName := "Internet gateway-" + vcnName
	subnetName := "public subnet-" + vcnName

	// 1. VCN with an Oracle-allocated IPv6 GUA /56
	vcnResp, err := netClient.CreateVcn(ctx, core.CreateVcnRequest{
		CreateVcnDetails: core.CreateVcnDetails{
			CompartmentId:                common.String(profile.TenancyOCID),
			DisplayName:                  common.String(vcnName),
			DnsLabel:                     common.String(DNSLabel("vcn", stamp)),
			CidrBlock:                    common.String("10.0.0.0/16"),
			IsIpv6Enabled:                common.Bool(true),
			IsOracleGuaAllocationEnabled: common.Bool(true),
		},
	})
	if err != nil {
		// Some regions/tenancies reject IPv6: fall back to an IPv4-only VCN.
		warnings = append(warnings, "IPv6 VCN 创建失败，已改为仅 IPv4: "+err.Error())
		vcnResp, err = netClient.CreateVcn(ctx, core.CreateVcnRequest{
			CreateVcnDetails: core.CreateVcnDetails{
				CompartmentId: common.String(profile.TenancyOCID),
				DisplayName:   common.String(vcnName),
				DnsLabel:      common.String(DNSLabel("vcn", stamp)),
				CidrBlock:     common.String("10.0.0.0/16"),
			},
		})
		if err != nil {
			return "", "", warnings, fmt.Errorf("failed to create VCN: %w", err)
		}
	}
	if vcnResp.Vcn.Id == nil {
		return "", "", warnings, fmt.Errorf("failed to create VCN: empty response")
	}
	vcnID = *vcnResp.Vcn.Id

	vcn, err := waitForVcnAvailable(ctx, netClient, vcnID, 60*time.Second)
	if err != nil {
		return vcnID, "", warnings, fmt.Errorf("VCN did not become available: %w", err)
	}
	ipv6Prefix := ""
	if len(vcn.Ipv6CidrBlocks) > 0 {
		ipv6Prefix = firstIPv6Subnet(vcn.Ipv6CidrBlocks[0])
	}
	if ipv6Prefix == "" {
		warnings = append(warnings, "VCN 未获得 IPv6 前缀，子网将只有 IPv4；实例的 IPv6 选项将不可用")
	}

	// 2. Internet Gateway
	igwResp, err := netClient.CreateInternetGateway(ctx, core.CreateInternetGatewayRequest{
		CreateInternetGatewayDetails: core.CreateInternetGatewayDetails{
			CompartmentId: common.String(profile.TenancyOCID),
			VcnId:         common.String(vcnID),
			DisplayName:   common.String(igwName),
			IsEnabled:     common.Bool(true),
		},
	})
	if err != nil || igwResp.InternetGateway.Id == nil {
		return vcnID, "", warnings, fmt.Errorf("failed to create Internet Gateway: %w", err)
	}
	igwID := *igwResp.InternetGateway.Id

	// 3. Default route table: IPv4 first (mandatory), then IPv6 (best effort)
	ipv4Route := core.RouteRule{
		Destination:     common.String("0.0.0.0/0"),
		DestinationType: core.RouteRuleDestinationTypeCidrBlock,
		NetworkEntityId: common.String(igwID),
	}
	ipv6Route := core.RouteRule{
		Destination:     common.String("::/0"),
		DestinationType: core.RouteRuleDestinationTypeCidrBlock,
		NetworkEntityId: common.String(igwID),
	}
	updateRoutes := func(rules []core.RouteRule) error {
		_, err := netClient.UpdateRouteTable(ctx, core.UpdateRouteTableRequest{
			RtId:                    vcn.DefaultRouteTableId,
			UpdateRouteTableDetails: core.UpdateRouteTableDetails{RouteRules: rules},
		})
		return err
	}
	if ipv6Prefix != "" {
		if err := updateRoutes([]core.RouteRule{ipv4Route, ipv6Route}); err != nil {
			warnings = append(warnings, "IPv6 默认路由添加失败: "+err.Error())
			if err := updateRoutes([]core.RouteRule{ipv4Route}); err != nil {
				return vcnID, "", warnings, fmt.Errorf("failed to set default route to Internet Gateway: %w", err)
			}
		}
	} else if err := updateRoutes([]core.RouteRule{ipv4Route}); err != nil {
		return vcnID, "", warnings, fmt.Errorf("failed to set default route to Internet Gateway: %w", err)
	}

	// 4. Default security list: keep the rules OCI created (SSH 22 and ICMP, like the console);
	//    a dual-stack subnet only needs the IPv6 egress rule added.
	if ipv6Prefix != "" {
		if err := ensureIPv6Egress(ctx, netClient, StrVal(vcn.DefaultSecurityListId)); err != nil {
			warnings = append(warnings, "IPv6 出站规则添加失败: "+err.Error())
		}
	}

	// 5. Public subnet (dual-stack when the VCN has an IPv6 prefix)
	subnetDetails := core.CreateSubnetDetails{
		CompartmentId:   common.String(profile.TenancyOCID),
		VcnId:           common.String(vcnID),
		DisplayName:     common.String(subnetName),
		DnsLabel:        common.String(DNSLabel("subnet", stamp)),
		CidrBlock:       common.String("10.0.0.0/24"),
		RouteTableId:    vcn.DefaultRouteTableId,
		SecurityListIds: []string{StrVal(vcn.DefaultSecurityListId)},
	}
	if ipv6Prefix != "" {
		subnetDetails.Ipv6CidrBlock = common.String(ipv6Prefix)
	}
	subResp, err := netClient.CreateSubnet(ctx, core.CreateSubnetRequest{CreateSubnetDetails: subnetDetails})
	if err != nil && ipv6Prefix != "" {
		warnings = append(warnings, "双栈子网创建失败，已改为仅 IPv4 子网: "+err.Error())
		subnetDetails.Ipv6CidrBlock = nil
		subResp, err = netClient.CreateSubnet(ctx, core.CreateSubnetRequest{CreateSubnetDetails: subnetDetails})
	}
	if err != nil || subResp.Subnet.Id == nil {
		return vcnID, "", warnings, fmt.Errorf("failed to create subnet: %w", err)
	}

	for _, w := range warnings {
		log.Printf("[Network] CreateRecommendedVCN warning (%s): %s", vcnID, w)
	}
	return vcnID, *subResp.Subnet.Id, warnings, nil
}

// ensureIPv6Egress adds an allow-all ::/0 egress rule to a security list unless one exists.
func ensureIPv6Egress(ctx context.Context, netClient core.VirtualNetworkClient, secListID string) error {
	resp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return err
	}
	for _, r := range resp.SecurityList.EgressSecurityRules {
		if StrVal(r.Destination) == "::/0" && StrVal(r.Protocol) == "all" {
			return nil
		}
	}
	egress := append([]core.EgressSecurityRule{}, resp.SecurityList.EgressSecurityRules...)
	egress = append(egress, core.EgressSecurityRule{
		Protocol:        common.String("all"),
		Destination:     common.String("::/0"),
		DestinationType: core.EgressSecurityRuleDestinationTypeCidrBlock,
	})
	_, err = netClient.UpdateSecurityList(ctx, core.UpdateSecurityListRequest{
		SecurityListId:            common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{EgressSecurityRules: egress},
	})
	return err
}

// ListSecurityRules lists ingress security rules of a security list
func ListSecurityRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string) ([]SecurityRuleItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	resp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return nil, err
	}

	items := make([]SecurityRuleItem, 0, len(resp.SecurityList.IngressSecurityRules))
	for _, rule := range resp.SecurityList.IngressSecurityRules {
		items = append(items, SecurityRuleItem{
			Key:         ruleKey(rule),
			Protocol:    protocolName(StrVal(rule.Protocol)),
			Source:      StrVal(rule.Source),
			Description: StrVal(rule.Description),
			PortRange:   describePorts(rule),
			IsStateless: BoolVal(rule.IsStateless),
		})
	}

	return items, nil
}

// SecurityListView is what the firewall page shows: both directions plus whether the list
// already is the minimal baseline (two ICMP rules in, everything out).
type SecurityListView struct {
	Ingress   []SecurityRuleItem `json:"ingress"`
	Egress    []SecurityRuleItem `json:"egress"`
	IsMinimal bool               `json:"is_minimal"`
	VcnCIDR   string             `json:"vcn_cidr"`
}

// DescribeSecurityList reads a security list and reports both rule directions.
func DescribeSecurityList(ctx context.Context, profile *storage.OCIProfile, region, secListID string) (*SecurityListView, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}
	resp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return nil, err
	}
	sl := resp.SecurityList

	view := &SecurityListView{Ingress: []SecurityRuleItem{}, Egress: []SecurityRuleItem{}}
	for _, rule := range sl.IngressSecurityRules {
		view.Ingress = append(view.Ingress, SecurityRuleItem{
			Key:         ruleKey(rule),
			Protocol:    protocolName(StrVal(rule.Protocol)),
			Source:      StrVal(rule.Source),
			Description: StrVal(rule.Description),
			PortRange:   describePorts(rule),
			IsStateless: BoolVal(rule.IsStateless),
		})
	}
	for _, rule := range sl.EgressSecurityRules {
		shaped := core.IngressSecurityRule{Protocol: rule.Protocol, TcpOptions: rule.TcpOptions, UdpOptions: rule.UdpOptions, IcmpOptions: rule.IcmpOptions}
		view.Egress = append(view.Egress, SecurityRuleItem{
			Protocol:    protocolName(StrVal(rule.Protocol)),
			Source:      StrVal(rule.Destination),
			Description: StrVal(rule.Description),
			PortRange:   describePorts(shaped),
			IsStateless: BoolVal(rule.IsStateless),
		})
	}

	if vcn, err := netClient.GetVcn(ctx, core.GetVcnRequest{VcnId: sl.VcnId}); err == nil {
		view.VcnCIDR = StrVal(vcn.Vcn.CidrBlock)
		if view.VcnCIDR == "" && len(vcn.Vcn.CidrBlocks) > 0 {
			view.VcnCIDR = vcn.Vcn.CidrBlocks[0]
		}
	}
	view.IsMinimal = isMinimalSecurityList(sl, view.VcnCIDR)
	return view, nil
}

// isMinimalSecurityList tells whether the list is exactly the baseline ResetSecurityListToMinimal
// writes: ingress ICMP 3/4 from 0.0.0.0/0 plus ICMP 3 from the VCN, egress allow-all only.
func isMinimalSecurityList(sl core.SecurityList, vcnCIDR string) bool {
	pmtud, internal := false, false
	for _, r := range sl.IngressSecurityRules {
		if StrVal(r.Protocol) != "1" || r.IcmpOptions == nil || r.IcmpOptions.Type == nil || *r.IcmpOptions.Type != 3 || BoolVal(r.IsStateless) {
			return false
		}
		switch {
		case StrVal(r.Source) == "0.0.0.0/0" && r.IcmpOptions.Code != nil && *r.IcmpOptions.Code == 4 && !pmtud:
			pmtud = true
		case vcnCIDR != "" && StrVal(r.Source) == vcnCIDR && r.IcmpOptions.Code == nil && !internal:
			internal = true
		default:
			return false
		}
	}
	if !pmtud || (vcnCIDR != "" && !internal) {
		return false
	}
	v4 := false
	for _, r := range sl.EgressSecurityRules {
		if StrVal(r.Protocol) != "all" {
			return false
		}
		switch StrVal(r.Destination) {
		case "0.0.0.0/0":
			v4 = true
		case "::/0":
		default:
			return false
		}
	}
	return v4
}

func protocolName(p string) string {
	switch p {
	case "6":
		return "TCP"
	case "17":
		return "UDP"
	case "1":
		return "ICMP"
	case "58":
		return "ICMPv6"
	case "all":
		return "ALL"
	}
	return p
}

func portRangeText(pr *core.PortRange) string {
	if pr == nil || pr.Min == nil || pr.Max == nil {
		return "ALL"
	}
	if *pr.Min == *pr.Max {
		return fmt.Sprintf("%d", *pr.Min)
	}
	return fmt.Sprintf("%d-%d", *pr.Min, *pr.Max)
}

func describePorts(rule core.IngressSecurityRule) string {
	switch {
	case rule.TcpOptions != nil:
		return portRangeText(rule.TcpOptions.DestinationPortRange)
	case rule.UdpOptions != nil:
		return portRangeText(rule.UdpOptions.DestinationPortRange)
	case rule.IcmpOptions != nil:
		if rule.IcmpOptions.Type == nil {
			return "ALL"
		}
		if rule.IcmpOptions.Code == nil {
			return fmt.Sprintf("type %d", *rule.IcmpOptions.Type)
		}
		return fmt.Sprintf("type %d code %d", *rule.IcmpOptions.Type, *rule.IcmpOptions.Code)
	}
	return "ALL"
}

// ruleKey identifies a rule by its effect so that repeated one-click actions do not duplicate it.
func ruleKey(r core.IngressSecurityRule) string {
	var b strings.Builder
	b.WriteString(StrVal(r.Protocol))
	b.WriteString("|")
	b.WriteString(StrVal(r.Source))
	b.WriteString("|")
	if r.TcpOptions != nil {
		b.WriteString("tcp:" + portRangeText(r.TcpOptions.DestinationPortRange))
	}
	if r.UdpOptions != nil {
		b.WriteString("udp:" + portRangeText(r.UdpOptions.DestinationPortRange))
	}
	if r.IcmpOptions != nil {
		b.WriteString("icmp:" + describePorts(r))
	}
	if BoolVal(r.IsStateless) {
		b.WriteString("|stateless")
	}
	return b.String()
}

// mergeIngressRules appends the wanted rules that are not already present.
func mergeIngressRules(existing, wanted []core.IngressSecurityRule) ([]core.IngressSecurityRule, int) {
	seen := make(map[string]bool, len(existing))
	for _, r := range existing {
		seen[ruleKey(r)] = true
	}
	added := 0
	out := append([]core.IngressSecurityRule{}, existing...)
	for _, r := range wanted {
		k := ruleKey(r)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
		added++
	}
	return out, added
}

func applyIngressRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string, wanted []core.IngressSecurityRule) (int, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return 0, err
	}

	getResp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return 0, err
	}

	merged, added := mergeIngressRules(getResp.SecurityList.IngressSecurityRules, wanted)
	if added == 0 {
		return 0, nil
	}
	if len(merged) > maxIngressRulesPerSecurityList {
		return 0, fmt.Errorf("安全列表最多 %d 条入站规则，当前 %d 条，再添加 %d 条会超限，请先清理规则",
			maxIngressRulesPerSecurityList, len(getResp.SecurityList.IngressSecurityRules), added)
	}

	// UpdateSecurityList replaces the whole ingress list; egress is left untouched (nil).
	_, err = netClient.UpdateSecurityList(ctx, core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: merged,
		},
	})
	if err != nil {
		return 0, err
	}
	return added, nil
}
