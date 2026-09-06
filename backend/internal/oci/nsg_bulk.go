package oci

import (
	"context"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

// Bulk helpers for the per-instance NSG: the shortcuts that used to act on the shared subnet
// security list now act on one instance's own group.

const nsgRulesPerRequest = 25 // the API accepts at most 25 rules per add / remove call

func nsgRuleKey(protocol, source, ports string) string {
	return protocol + "|" + source + "|" + ports
}

func toNSGRule(r core.IngressSecurityRule) core.AddSecurityRuleDetails {
	return core.AddSecurityRuleDetails{
		Direction:   core.AddSecurityRuleDetailsDirectionIngress,
		Protocol:    r.Protocol,
		Source:      r.Source,
		SourceType:  core.AddSecurityRuleDetailsSourceTypeCidrBlock,
		Description: r.Description,
		IsStateless: r.IsStateless,
		TcpOptions:  r.TcpOptions,
		UdpOptions:  r.UdpOptions,
		IcmpOptions: r.IcmpOptions,
	}
}

// addNSGRules adds the wanted rules that the group does not have yet, 25 per request.
func addNSGRules(ctx context.Context, netClient core.VirtualNetworkClient, nsgID string, wanted []core.IngressSecurityRule) (added int, err error) {
	existing, err := ListNSGIngressRules(ctx, netClient, nsgID)
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, e := range existing {
		have[nsgRuleKey(e.Protocol, e.Source, e.PortRange)] = true
	}
	var batch []core.AddSecurityRuleDetails
	for _, r := range wanted {
		k := nsgRuleKey(protocolName(StrVal(r.Protocol)), StrVal(r.Source), describePorts(r))
		if have[k] {
			continue
		}
		have[k] = true
		batch = append(batch, toNSGRule(r))
	}
	for i := 0; i < len(batch); i += nsgRulesPerRequest {
		end := min(i+nsgRulesPerRequest, len(batch))
		if _, err := netClient.AddNetworkSecurityGroupSecurityRules(ctx, core.AddNetworkSecurityGroupSecurityRulesRequest{
			NetworkSecurityGroupId:                      common.String(nsgID),
			AddNetworkSecurityGroupSecurityRulesDetails: core.AddNetworkSecurityGroupSecurityRulesDetails{SecurityRules: batch[i:end]},
		}); err != nil {
			return added, err
		}
		added += end - i
	}
	return added, nil
}

// AllowAllNSG adds allow-all ingress rules for 0.0.0.0/0 and ::/0 to the instance's group.
func AllowAllNSG(ctx context.Context, profile *storage.OCIProfile, region, nsgID string) (int, error) {
	return AllowAllNSGFor(ctx, profile, region, nsgID, true)
}

// AllowAllNSGFor adds the IPv4 allow-all rule and, when the instance has IPv6, the ::/0 one.
func AllowAllNSGFor(ctx context.Context, profile *storage.OCIProfile, region, nsgID string, includeIPv6 bool) (int, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return 0, err
	}
	wanted := []core.IngressSecurityRule{
		{Protocol: common.String("all"), Source: common.String("0.0.0.0/0"), SourceType: core.IngressSecurityRuleSourceTypeCidrBlock},
	}
	if includeIPv6 {
		wanted = append(wanted, core.IngressSecurityRule{Protocol: common.String("all"), Source: common.String("::/0"), SourceType: core.IngressSecurityRuleSourceTypeCidrBlock})
	}
	return addNSGRules(ctx, netClient, nsgID, wanted)
}

// AllowCloudflareNSG allows Cloudflare's IPv4 and IPv6 ranges on ports 80 and 443.
func AllowCloudflareNSG(ctx context.Context, profile *storage.OCIProfile, region, nsgID string) (int, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return 0, err
	}
	var wanted []core.IngressSecurityRule
	for _, cf := range CloudflareCIDRs {
		wanted = append(wanted, tcpRule(cf.CIDR, "", 80), tcpRule(cf.CIDR, "", 443))
	}
	return addNSGRules(ctx, netClient, nsgID, wanted)
}

// ClearNSGRules removes every ingress rule from the instance's group.
func ClearNSGRules(ctx context.Context, profile *storage.OCIProfile, region, nsgID string) (int, error) {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return 0, err
	}
	existing, err := ListNSGIngressRules(ctx, netClient, nsgID)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(existing))
	for _, e := range existing {
		if e.ID != "" {
			ids = append(ids, e.ID)
		}
	}
	removed := 0
	for i := 0; i < len(ids); i += nsgRulesPerRequest {
		end := min(i+nsgRulesPerRequest, len(ids))
		if _, err := netClient.RemoveNetworkSecurityGroupSecurityRules(ctx, core.RemoveNetworkSecurityGroupSecurityRulesRequest{
			NetworkSecurityGroupId:                         common.String(nsgID),
			RemoveNetworkSecurityGroupSecurityRulesDetails: core.RemoveNetworkSecurityGroupSecurityRulesDetails{SecurityRuleIds: ids[i:end]},
		}); err != nil {
			return removed, err
		}
		removed += end - i
	}
	return removed, nil
}

// ResetSecurityListToMinimal rewrites a subnet security list to the console default minus SSH:
// ingress only the two ICMP rules the console creates (path MTU discovery from anywhere,
// destination-unreachable from inside the VCN); egress everything, IPv6 too when the VCN has
// a prefix. With every instance on its own NSG, nothing in the shared list opens a port.
func ResetSecurityListToMinimal(ctx context.Context, profile *storage.OCIProfile, region, secListID string) error {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return err
	}
	sl, err := netClient.GetSecurityList(ctx, core.GetSecurityListRequest{SecurityListId: common.String(secListID)})
	if err != nil {
		return err
	}
	vcn, err := netClient.GetVcn(ctx, core.GetVcnRequest{VcnId: sl.SecurityList.VcnId})
	if err != nil {
		return err
	}
	vcnCIDR := StrVal(vcn.Vcn.CidrBlock)
	if vcnCIDR == "" && len(vcn.Vcn.CidrBlocks) > 0 {
		vcnCIDR = vcn.Vcn.CidrBlocks[0]
	}

	ingress := []core.IngressSecurityRule{
		{
			Protocol:    common.String("1"),
			Source:      common.String("0.0.0.0/0"),
			SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
			IcmpOptions: &core.IcmpOptions{Type: common.Int(3), Code: common.Int(4)},
		},
	}
	if vcnCIDR != "" {
		ingress = append(ingress, core.IngressSecurityRule{
			Protocol:    common.String("1"),
			Source:      common.String(vcnCIDR),
			SourceType:  core.IngressSecurityRuleSourceTypeCidrBlock,
			IcmpOptions: &core.IcmpOptions{Type: common.Int(3)},
		})
	}
	egress := []core.EgressSecurityRule{
		{Protocol: common.String("all"), Destination: common.String("0.0.0.0/0"), DestinationType: core.EgressSecurityRuleDestinationTypeCidrBlock},
	}
	if len(vcn.Vcn.Ipv6CidrBlocks) > 0 {
		egress = append(egress, core.EgressSecurityRule{Protocol: common.String("all"), Destination: common.String("::/0"), DestinationType: core.EgressSecurityRuleDestinationTypeCidrBlock})
	}

	_, err = netClient.UpdateSecurityList(ctx, core.UpdateSecurityListRequest{
		SecurityListId: common.String(secListID),
		UpdateSecurityListDetails: core.UpdateSecurityListDetails{
			IngressSecurityRules: ingress,
			EgressSecurityRules:  egress,
		},
	})
	return err
}
