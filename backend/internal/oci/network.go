package oci

import (
	"context"
	"fmt"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

// Cloudflare official IPv4 & IPv6 CIDR blocks
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
	State       string `json:"state"`
}

type SubnetItem struct {
	OCID           string `json:"ocid"`
	DisplayName    string `json:"display_name"`
	VcnID          string `json:"vcn_id"`
	CidrBlock      string `json:"cidr_block"`
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

// ListVCNs lists VCNs in the compartment
func ListVCNs(ctx context.Context, profile *storage.OCIProfile, region string) ([]VCNItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	req := core.ListVcnsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
	}

	resp, err := netClient.ListVcns(ctx, req)
	if err != nil {
		return nil, err
	}

	var items []VCNItem
	for _, vcn := range resp.Items {
		if vcn.LifecycleState == core.VcnLifecycleStateTerminated {
			continue
		}
		items = append(items, VCNItem{
			OCID:        StrVal(vcn.Id),
			DisplayName: StrVal(vcn.DisplayName),
			CidrBlock:   StrVal(vcn.CidrBlock),
			State:       string(vcn.LifecycleState),
		})
	}

	return items, nil
}

// ListSubnets lists subnets for a VCN
func ListSubnets(ctx context.Context, profile *storage.OCIProfile, region, vcnID string) ([]SubnetItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	req := core.ListSubnetsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		VcnId:         common.String(vcnID),
	}

	resp, err := netClient.ListSubnets(ctx, req)
	if err != nil {
		return nil, err
	}

	var items []SubnetItem
	for _, sub := range resp.Items {
		if sub.LifecycleState == core.SubnetLifecycleStateTerminated {
			continue
		}
		secListID := ""
		if len(sub.SecurityListIds) > 0 {
			secListID = sub.SecurityListIds[0]
		}
		items = append(items, SubnetItem{
			OCID:           StrVal(sub.Id),
			DisplayName:    StrVal(sub.DisplayName),
			VcnID:          StrVal(sub.VcnId),
			CidrBlock:      StrVal(sub.CidrBlock),
			SecurityListID: secListID,
			State:          string(sub.LifecycleState),
		})
	}

	return items, nil
}

// CreateRecommendedVCN creates VCN, IGW, Route Table (0.0.0.0/0 & ::/0), Security List (All common ports), and Subnet
func CreateRecommendedVCN(ctx context.Context, profile *storage.OCIProfile, region string) (string, string, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", "", err
	}

	// 1. Create VCN
	vcnReq := core.CreateVcnRequest{
		CreateVcnDetails: core.CreateVcnDetails{
			CompartmentId:                common.String(profile.TenancyOCID),
			DisplayName:                  common.String("oci-panel-default-vcn"),
			CidrBlock:                    common.String("10.0.0.0/16"),
			IsOracleGuaAllocationEnabled: common.Bool(true),
		},
	}
	vcnResp, err := netClient.CreateVcn(ctx, vcnReq)
	if err != nil || vcnResp.Vcn.Id == nil {
		return "", "", fmt.Errorf("failed to create VCN: %w", err)
	}
	vcnID := *vcnResp.Vcn.Id

	// 2. Create Internet Gateway
	igwReq := core.CreateInternetGatewayRequest{
		CreateInternetGatewayDetails: core.CreateInternetGatewayDetails{
			CompartmentId: common.String(profile.TenancyOCID),
			VcnId:         common.String(vcnID),
			DisplayName:   common.String("oci-panel-igw"),
			IsEnabled:     common.Bool(true),
		},
	}
	igwResp, err := netClient.CreateInternetGateway(ctx, igwReq)
	if err != nil || igwResp.InternetGateway.Id == nil {
		return "", "", fmt.Errorf("failed to create Internet Gateway: %w", err)
	}
	igwID := *igwResp.InternetGateway.Id

	// 3. Update Default Route Table (add 0.0.0.0/0 -> IGW and ::/0 -> IGW)
	routeReq := core.UpdateRouteTableRequest{
		RtId: vcnResp.Vcn.DefaultRouteTableId,
		UpdateRouteTableDetails: core.UpdateRouteTableDetails{
			RouteRules: []core.RouteRule{
				{
					Destination:       common.String("0.0.0.0/0"),
					DestinationType:   core.RouteRuleDestinationTypeCidrBlock,
					NetworkEntityId:   common.String(igwID),
					Description:       common.String("Default IPv4 public internet route"),
				},
				{
					Destination:       common.String("::/0"),
					DestinationType:   core.RouteRuleDestinationTypeCidrBlock,
					NetworkEntityId:   common.String(igwID),
					Description:       common.String("Default IPv6 public internet route"),
				},
			},
		},
	}
	_, _ = netClient.UpdateRouteTable(ctx, routeReq)

	// 4. Update Default Security List (Allow SSH, HTTP, HTTPS, ICMP, WireGuard, all outbound)
	secListReq := core.UpdateSecurityListRequest{
		SecurityListId: vcnResp.Vcn.DefaultSecurityListId,
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: []core.IngressSecurityRule{
				// SSH
				{
					Protocol:    common.String("6"), // TCP
					Source:      common.String("0.0.0.0/0"),
					Description: common.String("Allow SSH 22"),
					TcpOptions: &core.TcpOptions{
						DestinationPortRange: &core.PortRange{Min: common.Int(22), Max: common.Int(22)},
					},
				},
				// Web 80, 443
				{
					Protocol:    common.String("6"),
					Source:      common.String("0.0.0.0/0"),
					Description: common.String("Allow Web HTTP/HTTPS"),
					TcpOptions: &core.TcpOptions{
						DestinationPortRange: &core.PortRange{Min: common.Int(80), Max: common.Int(443)},
					},
				},
				// Ping (ICMP)
				{
					Protocol:    common.String("1"), // ICMP
					Source:      common.String("0.0.0.0/0"),
					Description: common.String("Allow ICMP Echo Ping"),
					IcmpOptions: &core.IcmpOptions{Type: common.Int(8), Code: common.Int(0)},
				},
				// IPv6 All
				{
					Protocol:    common.String("all"),
					Source:      common.String("::/0"),
					Description: common.String("Allow all IPv6 traffic"),
				},
			},
			EgressSecurityRules: []core.EgressSecurityRule{
				{
					Protocol:    common.String("all"),
					Destination: common.String("0.0.0.0/0"),
					Description: common.String("Allow all IPv4 outbound"),
				},
				{
					Protocol:    common.String("all"),
					Destination: common.String("::/0"),
					Description: common.String("Allow all IPv6 outbound"),
				},
			},
		},
	}
	_, _ = netClient.UpdateSecurityList(ctx, secListReq)

	// 5. Create Public Subnet
	subReq := core.CreateSubnetRequest{
		CreateSubnetDetails: core.CreateSubnetDetails{
			CompartmentId: common.String(profile.TenancyOCID),
			VcnId:         common.String(vcnID),
			DisplayName:   common.String("oci-panel-default-subnet"),
			CidrBlock:     common.String("10.0.0.0/24"),
			RouteTableId:  vcnResp.Vcn.DefaultRouteTableId,
			SecurityListIds: []string{
				*vcnResp.Vcn.DefaultSecurityListId,
			},
		},
	}
	subResp, err := netClient.CreateSubnet(ctx, subReq)
	if err != nil || subResp.Subnet.Id == nil {
		return "", "", fmt.Errorf("failed to create Subnet: %w", err)
	}

	return vcnID, *subResp.Subnet.Id, nil
}

// ListSecurityRules lists ingress security rules in a Security List
func ListSecurityRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string) ([]SecurityRuleItem, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	resp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return nil, err
	}

	var items []SecurityRuleItem
	for _, rule := range resp.SecurityList.IngressSecurityRules {
		portStr := "ALL"
		if rule.TcpOptions != nil && rule.TcpOptions.DestinationPortRange != nil {
			minP := *rule.TcpOptions.DestinationPortRange.Min
			maxP := *rule.TcpOptions.DestinationPortRange.Max
			if minP == maxP {
				portStr = fmt.Sprintf("%d", minP)
			} else {
				portStr = fmt.Sprintf("%d-%d", minP, maxP)
			}
		}

		proto := StrVal(rule.Protocol)
		if proto == "6" {
			proto = "TCP"
		} else if proto == "17" {
			proto = "UDP"
		} else if proto == "1" {
			proto = "ICMP"
		}

		items = append(items, SecurityRuleItem{
			Protocol:    proto,
			Source:      StrVal(rule.Source),
			Description: StrVal(rule.Description),
			PortRange:   portStr,
		})
	}

	return items, nil
}

// AllowAllFirewallRules adds 0.0.0.0/0 and ::/0 allow-all ingress rules
func AllowAllFirewallRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return err
	}

	getResp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return err
	}

	rules := getResp.SecurityList.IngressSecurityRules
	// Append All-Traffic rules
	rules = append(rules, core.IngressSecurityRule{
		Protocol:    common.String("all"),
		Source:      common.String("0.0.0.0/0"),
		Description: common.String("Allow All IPv4 (One-Click Allow-All)"),
	})
	rules = append(rules, core.IngressSecurityRule{
		Protocol:    common.String("all"),
		Source:      common.String("::/0"),
		Description: common.String("Allow All IPv6 (One-Click Allow-All)"),
	})

	updateReq := core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: rules,
		},
	}

	_, err = netClient.UpdateSecurityList(ctx, updateReq)
	return err
}

// ClearAllFirewallRules deletes all ingress rules
func ClearAllFirewallRules(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return err
	}

	updateReq := core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: []core.IngressSecurityRule{}, // Clear all
		},
	}

	_, err = netClient.UpdateSecurityList(ctx, updateReq)
	return err
}

// AllowCloudflareCDNIPs adds Cloudflare official IPv4 & IPv6 CIDRs to ports 80 and 443
func AllowCloudflareCDNIPs(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return err
	}

	getResp, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return err
	}

	rules := getResp.SecurityList.IngressSecurityRules

	// Add Cloudflare CIDRs for HTTP/HTTPS
	for _, cf := range CloudflareCIDRs {
		// Port 80
		rules = append(rules, core.IngressSecurityRule{
			Protocol:    common.String("6"), // TCP
			Source:      common.String(cf.CIDR),
			Description: common.String("Cloudflare CDN Ingress Port 80"),
			TcpOptions: &core.TcpOptions{
				DestinationPortRange: &core.PortRange{Min: common.Int(80), Max: common.Int(80)},
			},
		})
		// Port 443
		rules = append(rules, core.IngressSecurityRule{
			Protocol:    common.String("6"), // TCP
			Source:      common.String(cf.CIDR),
			Description: common.String("Cloudflare CDN Ingress Port 443"),
			TcpOptions: &core.TcpOptions{
				DestinationPortRange: &core.PortRange{Min: common.Int(443), Max: common.Int(443)},
			},
		})
	}

	updateReq := core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: rules,
		},
	}

	_, err = netClient.UpdateSecurityList(ctx, updateReq)
	return err
}
