package oci

import (
	"context"
	"fmt"
	"log"
	"strings"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

// Per-instance firewall = one Network Security Group attached to the instance's primary VNIC.
// Security lists stay on the subnet (shared by every instance in it); the NSG carries the rules
// that belong to this instance only. OCI allows a packet when either the security list or an
// NSG allows it, so the subnet list is kept minimal and the instance's ports live here.

type NSGRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NSGRuleItem struct {
	ID          string `json:"id"`
	Protocol    string `json:"protocol"`
	Source      string `json:"source"`
	PortRange   string `json:"port_range"`
	Description string `json:"description"`
	IsStateless bool   `json:"is_stateless"`
}

type InstanceFirewall struct {
	VnicID        string        `json:"vnic_id"`
	NSG           *NSGRef       `json:"nsg"` // nil until enabled
	OtherNSGCount int           `json:"other_nsg_count"`
	Rules         []NSGRuleItem `json:"rules"`
}

func instanceVnic(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (core.ComputeClient, core.VirtualNetworkClient, core.Vnic, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return core.ComputeClient{}, core.VirtualNetworkClient{}, core.Vnic{}, err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return core.ComputeClient{}, core.VirtualNetworkClient{}, core.Vnic{}, err
	}
	vnic, err := primaryVnic(ctx, computeClient, netClient, instanceCompartment(ctx, computeClient, profile, instanceOCID), instanceOCID)
	if err != nil {
		return core.ComputeClient{}, core.VirtualNetworkClient{}, core.Vnic{}, fmt.Errorf("unable to find VNIC for instance: %w", err)
	}
	return computeClient, netClient, vnic, nil
}

// GetInstanceFirewall returns the NSG attached to the instance's primary VNIC (the first one,
// when several are attached) and its ingress rules.
func GetInstanceFirewall(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (*InstanceFirewall, error) {
	_, netClient, vnic, err := instanceVnic(ctx, profile, region, instanceOCID)
	if err != nil {
		return nil, err
	}
	return describeFirewall(ctx, netClient, vnic)
}

func describeFirewall(ctx context.Context, netClient core.VirtualNetworkClient, vnic core.Vnic) (*InstanceFirewall, error) {
	fw := &InstanceFirewall{VnicID: StrVal(vnic.Id), Rules: []NSGRuleItem{}}
	if len(vnic.NsgIds) == 0 {
		return fw, nil
	}
	nsgID := vnic.NsgIds[0]
	fw.OtherNSGCount = len(vnic.NsgIds) - 1

	name := ""
	if resp, err := netClient.GetNetworkSecurityGroup(ctx, core.GetNetworkSecurityGroupRequest{NetworkSecurityGroupId: common.String(nsgID)}); err == nil {
		name = StrVal(resp.NetworkSecurityGroup.DisplayName)
	}
	fw.NSG = &NSGRef{ID: nsgID, Name: name}

	rules, err := ListNSGIngressRules(ctx, netClient, nsgID)
	if err != nil {
		return nil, err
	}
	fw.Rules = rules
	return fw, nil
}

// EnsureInstanceNSG creates a console-style named NSG in the VNIC's VCN and attaches it to the
// primary VNIC, unless the VNIC already has one.
func EnsureInstanceNSG(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (*InstanceFirewall, error) {
	_, netClient, vnic, err := instanceVnic(ctx, profile, region, instanceOCID)
	if err != nil {
		return nil, err
	}
	if len(vnic.NsgIds) > 0 {
		return describeFirewall(ctx, netClient, vnic)
	}

	subnet, err := netClient.GetSubnet(ctx, core.GetSubnetRequest{SubnetId: vnic.SubnetId})
	if err != nil {
		return nil, fmt.Errorf("failed to read the instance's subnet: %w", err)
	}
	created, err := netClient.CreateNetworkSecurityGroup(ctx, core.CreateNetworkSecurityGroupRequest{
		CreateNetworkSecurityGroupDetails: core.CreateNetworkSecurityGroupDetails{
			CompartmentId: vnic.CompartmentId,
			VcnId:         subnet.Subnet.VcnId,
			DisplayName:   common.String(DefaultName("nsg")),
		},
	})
	if err != nil || created.NetworkSecurityGroup.Id == nil {
		return nil, fmt.Errorf("failed to create network security group: %w", err)
	}
	nsgID := *created.NetworkSecurityGroup.Id

	if _, err := netClient.UpdateVnic(ctx, core.UpdateVnicRequest{
		VnicId:            vnic.Id,
		UpdateVnicDetails: core.UpdateVnicDetails{NsgIds: []string{nsgID}},
	}); err != nil {
		// Do not leave an orphan behind
		_, _ = netClient.DeleteNetworkSecurityGroup(ctx, core.DeleteNetworkSecurityGroupRequest{NetworkSecurityGroupId: common.String(nsgID)})
		return nil, fmt.Errorf("failed to attach the network security group to the VNIC: %w", err)
	}

	vnic.NsgIds = []string{nsgID}
	return describeFirewall(ctx, netClient, vnic)
}

// ListNSGIngressRules lists the ingress rules of one NSG (all pages).
func ListNSGIngressRules(ctx context.Context, netClient core.VirtualNetworkClient, nsgID string) ([]NSGRuleItem, error) {
	req := core.ListNetworkSecurityGroupSecurityRulesRequest{
		NetworkSecurityGroupId: common.String(nsgID),
		Direction:              core.ListNetworkSecurityGroupSecurityRulesDirectionIngress,
		Limit:                  common.Int(100),
	}
	items := []NSGRuleItem{}
	for {
		resp, err := netClient.ListNetworkSecurityGroupSecurityRules(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, r := range resp.Items {
			shaped := core.IngressSecurityRule{Protocol: r.Protocol, TcpOptions: r.TcpOptions, UdpOptions: r.UdpOptions, IcmpOptions: r.IcmpOptions}
			items = append(items, NSGRuleItem{
				ID:          StrVal(r.Id),
				Protocol:    protocolName(StrVal(r.Protocol)),
				Source:      StrVal(r.Source),
				PortRange:   describePorts(shaped),
				Description: StrVal(r.Description),
				IsStateless: BoolVal(r.IsStateless),
			})
		}
		if resp.OpcNextPage == nil {
			break
		}
		req.Page = resp.OpcNextPage
	}
	return items, nil
}

// AddNSGRule adds one ingress rule to the NSG; an identical existing rule is not duplicated.
func AddNSGRule(ctx context.Context, profile *storage.OCIProfile, region, nsgID string, spec IngressRuleSpec) (added bool, err error) {
	rule, err := BuildIngressRule(spec)
	if err != nil {
		return false, err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return false, err
	}

	existing, err := ListNSGIngressRules(ctx, netClient, nsgID)
	if err != nil {
		return false, err
	}
	wantKey := strings.Join([]string{protocolName(StrVal(rule.Protocol)), StrVal(rule.Source), describePorts(rule)}, "|")
	for _, e := range existing {
		if strings.Join([]string{e.Protocol, e.Source, e.PortRange}, "|") == wantKey {
			return false, nil
		}
	}

	_, err = netClient.AddNetworkSecurityGroupSecurityRules(ctx, core.AddNetworkSecurityGroupSecurityRulesRequest{
		NetworkSecurityGroupId: common.String(nsgID),
		AddNetworkSecurityGroupSecurityRulesDetails: core.AddNetworkSecurityGroupSecurityRulesDetails{
			SecurityRules: []core.AddSecurityRuleDetails{{
				Direction:   core.AddSecurityRuleDetailsDirectionIngress,
				Protocol:    rule.Protocol,
				Source:      rule.Source,
				SourceType:  core.AddSecurityRuleDetailsSourceTypeCidrBlock,
				Description: rule.Description,
				IsStateless: rule.IsStateless,
				TcpOptions:  rule.TcpOptions,
				UdpOptions:  rule.UdpOptions,
				IcmpOptions: rule.IcmpOptions,
			}},
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// DeleteNSGRule removes one rule by id.
func DeleteNSGRule(ctx context.Context, profile *storage.OCIProfile, region, nsgID, ruleID string) error {
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return err
	}
	_, err = netClient.RemoveNetworkSecurityGroupSecurityRules(ctx, core.RemoveNetworkSecurityGroupSecurityRulesRequest{
		NetworkSecurityGroupId: common.String(nsgID),
		RemoveNetworkSecurityGroupSecurityRulesDetails: core.RemoveNetworkSecurityGroupSecurityRulesDetails{
			SecurityRuleIds: []string{ruleID},
		},
	})
	return err
}

// RemoveInstanceNSG detaches the NSG from the instance's primary VNIC and deletes it. When the
// group is still used by another VNIC the deletion is skipped and reported.
func RemoveInstanceNSG(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID, nsgID string) (deleted bool, err error) {
	_, netClient, vnic, err := instanceVnic(ctx, profile, region, instanceOCID)
	if err != nil {
		return false, err
	}
	remaining := make([]string, 0, len(vnic.NsgIds))
	for _, id := range vnic.NsgIds {
		if id != nsgID {
			remaining = append(remaining, id)
		}
	}
	if _, err := netClient.UpdateVnic(ctx, core.UpdateVnicRequest{
		VnicId:            vnic.Id,
		UpdateVnicDetails: core.UpdateVnicDetails{NsgIds: remaining},
	}); err != nil {
		return false, fmt.Errorf("failed to detach the network security group: %w", err)
	}
	if _, err := netClient.DeleteNetworkSecurityGroup(ctx, core.DeleteNetworkSecurityGroupRequest{NetworkSecurityGroupId: common.String(nsgID)}); err != nil {
		log.Printf("[Network] NSG %s detached but not deleted (still in use elsewhere?): %v", nsgID, err)
		return false, nil
	}
	return true, nil
}
