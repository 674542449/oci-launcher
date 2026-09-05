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
	Protocol    string `json:"protocol"`
	Source      string `json:"source"`
	Description string `json:"description"`
	PortRange   string `json:"port_range"`
	IsStateless bool   `json:"is_stateless"`
}

// ListVCNs lists VCNs in the tenancy root compartment (all pages)
func ListVCNs(ctx context.Context, profile *storage.OCIProfile, region string) ([]VCNItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	req := core.ListVcnsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		Limit:         common.Int(100),
	}

	var items []VCNItem
	for {
		resp, err := netClient.ListVcns(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, vcn := range resp.Items {
			if vcn.LifecycleState == core.VcnLifecycleStateTerminated || vcn.LifecycleState == core.VcnLifecycleStateTerminating {
				continue
			}
			items = append(items, VCNItem{
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

	return items, nil
}

// ListSubnets lists subnets for a VCN (all pages)
func ListSubnets(ctx context.Context, profile *storage.OCIProfile, region, vcnID string) ([]SubnetItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	req := core.ListSubnetsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		VcnId:         common.String(vcnID),
		Limit:         common.Int(100),
	}

	var items []SubnetItem
	for {
		resp, err := netClient.ListSubnets(ctx, req)
		if err != nil {
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
	return core.IngressSecurityRule{
		Protocol:    common.String("6"),
		Source:      common.String(source),
		SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
		Description: common.String(desc),
		TcpOptions: &core.TcpOptions{
			DestinationPortRange: &core.PortRange{Min: common.Int(port), Max: common.Int(port)},
		},
	}
}

// CreateRecommendedVCN creates an IPv6-enabled VCN, an Internet Gateway, default routes,
// a sane default security list (22/80/443, ping, PMTUD, all IPv6) and one public subnet.
// Non-fatal problems (e.g. IPv6 not available in the region) are returned as warnings.
func CreateRecommendedVCN(ctx context.Context, profile *storage.OCIProfile, region string) (vcnID, subnetID string, warnings []string, err error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", "", nil, err
	}

	// 1. VCN with an Oracle-allocated IPv6 GUA /56
	vcnResp, err := netClient.CreateVcn(ctx, core.CreateVcnRequest{
		CreateVcnDetails: core.CreateVcnDetails{
			CompartmentId:                common.String(profile.TenancyOCID),
			DisplayName:                  common.String("oci-panel-default-vcn"),
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
				DisplayName:   common.String("oci-panel-default-vcn"),
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
			DisplayName:   common.String("oci-panel-igw"),
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
		Description:     common.String("Default IPv4 internet route"),
	}
	ipv6Route := core.RouteRule{
		Destination:     common.String("::/0"),
		DestinationType: core.RouteRuleDestinationTypeCidrBlock,
		NetworkEntityId: common.String(igwID),
		Description:     common.String("Default IPv6 internet route"),
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

	// 4. Default security list
	ingress := []core.IngressSecurityRule{
		tcpRule("0.0.0.0/0", "Allow SSH", 22),
		tcpRule("0.0.0.0/0", "Allow HTTP", 80),
		tcpRule("0.0.0.0/0", "Allow HTTPS", 443),
		{
			Protocol:    common.String("1"),
			Source:      common.String("0.0.0.0/0"),
			SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
			Description: common.String("Allow ICMP echo (ping)"),
			IcmpOptions: &core.IcmpOptions{Type: common.Int(8)},
		},
		{
			Protocol:    common.String("1"),
			Source:      common.String("0.0.0.0/0"),
			SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
			Description: common.String("ICMP path MTU discovery"),
			IcmpOptions: &core.IcmpOptions{Type: common.Int(3), Code: common.Int(4)},
		},
	}
	ipv6Ingress := core.IngressSecurityRule{
		Protocol:    common.String("all"),
		Source:      common.String("::/0"),
		SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
		Description: common.String("Allow all IPv6 inbound"),
	}
	egress := []core.EgressSecurityRule{
		{
			Protocol:        common.String("all"),
			Destination:     common.String("0.0.0.0/0"),
			DestinationType: core.EgressSecurityRuleDestinationTypeCidrBlock,
			Description:     common.String("Allow all IPv4 outbound"),
		},
	}
	ipv6Egress := core.EgressSecurityRule{
		Protocol:        common.String("all"),
		Destination:     common.String("::/0"),
		DestinationType: core.EgressSecurityRuleDestinationTypeCidrBlock,
		Description:     common.String("Allow all IPv6 outbound"),
	}
	updateSecList := func(in []core.IngressSecurityRule, out []core.EgressSecurityRule) error {
		_, err := netClient.UpdateSecurityList(ctx, core.UpdateSecurityListRequest{
			SecurityListId: vcn.DefaultSecurityListId,
			UpdateSecurityListDetails: core.UpdateSecurityListDetails{
				IngressSecurityRules: in,
				EgressSecurityRules:  out,
			},
		})
		return err
	}
	if ipv6Prefix != "" {
		if err := updateSecList(append(append([]core.IngressSecurityRule{}, ingress...), ipv6Ingress), append(append([]core.EgressSecurityRule{}, egress...), ipv6Egress)); err != nil {
			warnings = append(warnings, "IPv6 安全规则添加失败: "+err.Error())
			if err := updateSecList(ingress, egress); err != nil {
				return vcnID, "", warnings, fmt.Errorf("failed to update default security list: %w", err)
			}
		}
	} else if err := updateSecList(ingress, egress); err != nil {
		return vcnID, "", warnings, fmt.Errorf("failed to update default security list: %w", err)
	}

	// 5. Public subnet (dual-stack when the VCN has an IPv6 prefix)
	subnetDetails := core.CreateSubnetDetails{
		CompartmentId:   common.String(profile.TenancyOCID),
		VcnId:           common.String(vcnID),
		DisplayName:     common.String("oci-panel-default-subnet"),
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
			Protocol:    protocolName(StrVal(rule.Protocol)),
			Source:      StrVal(rule.Source),
			Description: StrVal(rule.Description),
			PortRange:   describePorts(rule),
			IsStateless: BoolVal(rule.IsStateless),
		})
	}

	return items, nil
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

// AllowAllFirewallRules adds 0.0.0.0/0 and ::/0 allow-all ingress rules (idempotent)
func AllowAllFirewallRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	wanted := []core.IngressSecurityRule{
		{
			Protocol:    common.String("all"),
			Source:      common.String("0.0.0.0/0"),
			SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
			Description: common.String("Allow All IPv4 (One-Click Allow-All)"),
		},
		{
			Protocol:    common.String("all"),
			Source:      common.String("::/0"),
			SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
			Description: common.String("Allow All IPv6 (One-Click Allow-All)"),
		},
	}
	_, err := applyIngressRules(ctx, profile, region, secListID, wanted)
	return err
}

// ClearAllFirewallRules deletes all ingress rules (egress untouched)
func ClearAllFirewallRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return err
	}

	_, err = netClient.UpdateSecurityList(ctx, core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: []core.IngressSecurityRule{},
		},
	})
	return err
}

// AllowCloudflareCDNIPs allows Cloudflare's IPv4 & IPv6 ranges on ports 80 and 443 (idempotent)
func AllowCloudflareCDNIPs(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	var wanted []core.IngressSecurityRule
	for _, cf := range CloudflareCIDRs {
		wanted = append(wanted,
			tcpRule(cf.CIDR, "Cloudflare CDN Ingress Port 80", 80),
			tcpRule(cf.CIDR, "Cloudflare CDN Ingress Port 443", 443),
		)
	}
	_, err := applyIngressRules(ctx, profile, region, secListID, wanted)
	return err
}
