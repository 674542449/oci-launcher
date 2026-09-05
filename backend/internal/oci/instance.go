package oci

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type InstanceItem struct {
	OCID         string            `json:"ocid"`
	DisplayName  string            `json:"display_name"`
	Shape        string            `json:"shape"`
	OCPU         float32           `json:"ocpu"`
	MemoryInGBs  float32           `json:"memory_in_gbs"`
	State        string            `json:"state"`
	AD           string            `json:"ad"`
	Region       string            `json:"region"`
	PublicIP     string            `json:"public_ip"`
	PrivateIP    string            `json:"private_ip"`
	IPv6         string            `json:"ipv6"`
	TimeCreated  string            `json:"time_created"`
	FreeformTags map[string]string `json:"freeform_tags"`
	RootPassword string            `json:"root_password,omitempty"`
	SSHCommand   string            `json:"ssh_command"`
}

// ListInstancesWithDetails retrieves instances with VNIC details (Public IP, IPv6, Tags)
func ListInstancesWithDetails(ctx context.Context, profile *storage.OCIProfile, region string) ([]InstanceItem, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return nil, err
	}

	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	req := core.ListInstancesRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		SortBy:        core.ListInstancesSortByTimecreated,
		SortOrder:     core.ListInstancesSortOrderDesc,
	}

	resp, err := computeClient.ListInstances(ctx, req)
	if err != nil {
		return nil, err
	}

	var items []InstanceItem
	for _, inst := range resp.Items {
		if inst.LifecycleState == core.InstanceLifecycleStateTerminated {
			continue
		}

		item := InstanceItem{
			OCID:         StrVal(inst.Id),
			DisplayName:  StrVal(inst.DisplayName),
			Shape:        StrVal(inst.Shape),
			State:        string(inst.LifecycleState),
			AD:           StrVal(inst.AvailabilityDomain),
			Region:       region,
			FreeformTags: inst.FreeformTags,
		}

		if inst.ShapeConfig != nil {
			if inst.ShapeConfig.Ocpus != nil {
				item.OCPU = *inst.ShapeConfig.Ocpus
			}
			if inst.ShapeConfig.MemoryInGBs != nil {
				item.MemoryInGBs = *inst.ShapeConfig.MemoryInGBs
			}
		}

		if inst.TimeCreated != nil {
			item.TimeCreated = inst.TimeCreated.Format("2006-01-02 15:04:05")
		}

		// Read root password from freeform tags if present
		if inst.FreeformTags != nil {
			if pass, ok := inst.FreeformTags["root_password"]; ok {
				item.RootPassword = pass
			}
		}

		// Query VNIC for IP addresses
		vnicReq := core.ListVnicAttachmentsRequest{
			CompartmentId: common.String(profile.TenancyOCID),
			InstanceId:    inst.Id,
		}
		vnicResp, err2 := computeClient.ListVnicAttachments(ctx, vnicReq)
		if err2 == nil && len(vnicResp.Items) > 0 {
			for _, va := range vnicResp.Items {
				if va.LifecycleState == core.VnicAttachmentLifecycleStateAttached && va.VnicId != nil {
					vnicDetail, err3 := netClient.GetVnic(ctx, core.GetVnicRequest{VnicId: va.VnicId})
					if err3 == nil {
						item.PublicIP = StrVal(vnicDetail.Vnic.PublicIp)
						item.PrivateIP = StrVal(vnicDetail.Vnic.PrivateIp)
						if len(vnicDetail.Vnic.Ipv6Addresses) > 0 {
							item.IPv6 = vnicDetail.Vnic.Ipv6Addresses[0]
						}
						break
					}
				}
			}
		}

		if item.PublicIP != "" {
			item.SSHCommand = fmt.Sprintf("ssh root@%s", item.PublicIP)
		}

		items = append(items, item)
	}

	return items, nil
}

// BuildCloudInitUserData generates cloud-init base64 script
func BuildCloudInitUserData(loginMode, sshKey, rootPassword string, enableBBR bool) string {
	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString("echo '=== Initializing OCI Instance ===' > /var/log/oci_init.log\n")

	// 1. Flush & disable internal system firewalls completely
	script.WriteString("# Flush internal firewalls\n")
	script.WriteString("iptables -P INPUT ACCEPT || true\n")
	script.WriteString("iptables -P FORWARD ACCEPT || true\n")
	script.WriteString("iptables -P OUTPUT ACCEPT || true\n")
	script.WriteString("iptables -F || true\n")
	script.WriteString("iptables -X || true\n")
	script.WriteString("netfilter-persistent save || true\n")
	script.WriteString("ufw disable || true\n")
	script.WriteString("systemctl stop firewalld || true\n")
	script.WriteString("systemctl disable firewalld || true\n")

	// 2. Configure SSHD
	script.WriteString("mkdir -p /root/.ssh && chmod 700 /root/.ssh\n")

	if loginMode == "root_password" && rootPassword != "" {
		script.WriteString(fmt.Sprintf("echo 'root:%s' | chpasswd\n", rootPassword))
		script.WriteString("sed -i 's/^#*PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config\n")
		script.WriteString("sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config\n")
		script.WriteString("echo 'PermitRootLogin yes' >> /etc/ssh/sshd_config.d/60-cloudimg-settings.conf 2>/dev/null || true\n")
		script.WriteString("echo 'PasswordAuthentication yes' >> /etc/ssh/sshd_config.d/60-cloudimg-settings.conf 2>/dev/null || true\n")
	} else if sshKey != "" {
		script.WriteString(fmt.Sprintf("echo '%s' >> /root/.ssh/authorized_keys\n", strings.TrimSpace(sshKey)))
		script.WriteString("chmod 600 /root/.ssh/authorized_keys\n")
		script.WriteString("sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config\n")
	}

	script.WriteString("systemctl restart ssh || systemctl restart sshd || true\n")

	// 3. Enable BBR
	if enableBBR {
		script.WriteString("# Enable BBR\n")
		script.WriteString("echo 'net.core.default_qdisc=fq' >> /etc/sysctl.conf\n")
		script.WriteString("echo 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.conf\n")
		script.WriteString("sysctl -p || true\n")
	}

	return base64.StdEncoding.EncodeToString([]byte(script.String()))
}

// LaunchInstance launches a compute instance with Cloud-Init, VPU 120, and tags
func LaunchInstance(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask, targetAD string) (string, error) {
	computeClient, err := GetComputeClient(profile, task.Region)
	if err != nil {
		return "", err
	}

	// Prepare Freeform Tags
	tags := map[string]string{
		"created_by": "oci-panel",
	}
	if task.LoginMode == "root_password" && task.RootPasswordEnc != "" {
		tags["root_password"] = task.RootPasswordEnc
	}

	// Prepare User Data
	userDataB64 := BuildCloudInitUserData(task.LoginMode, task.SSHAuthorizedKeys, task.RootPasswordEnc, true)

	vpu := task.BootVolumeVPU
	if vpu < 10 {
		vpu = 120 // Default 120 VPU Ultra High Performance (Boot volumes strictly require minimum 10 VPU)
	}
	if vpu > 120 {
		vpu = 120
	}

	// Launch Instance Details
	details := core.LaunchInstanceDetails{
		CompartmentId:      common.String(profile.TenancyOCID),
		AvailabilityDomain: common.String(targetAD),
		DisplayName:        common.String(task.InstanceName),
		Shape:              common.String(task.Shape),
		SourceDetails: core.InstanceSourceViaImageDetails{
			ImageId:             common.String(task.ImageOCID),
			BootVolumeSizeInGBs: common.Int64(task.BootVolumeSizeInGBs),
			BootVolumeVpusPerGB: common.Int64(vpu),
		},
		CreateVnicDetails: &core.CreateVnicDetails{
			SubnetId:       common.String(task.SubnetOCID),
			AssignPublicIp: common.Bool(task.AssignPublicIP),
			AssignIpv6Ip:   common.Bool(task.EnableIPv6),
			DisplayName:    common.String(task.InstanceName + "-vnic"),
		},
		Metadata: map[string]string{
			"user_data": userDataB64,
		},
		FreeformTags: tags,
	}

	// Set shape config for flex shapes
	if strings.Contains(task.Shape, "Flex") {
		details.ShapeConfig = &core.LaunchInstanceShapeConfigDetails{
			Ocpus:       common.Float32(float32(task.OCPU)),
			MemoryInGBs: common.Float32(float32(task.MemoryInGBs)),
		}
	}

	req := core.LaunchInstanceRequest{
		LaunchInstanceDetails: details,
	}

	resp, err := computeClient.LaunchInstance(ctx, req)
	if err != nil {
		return "", err
	}

	return StrVal(resp.Instance.Id), nil
}

// InstanceAction performs START, STOP, SOFTRESET, RESET
func InstanceAction(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID, action string) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	var ociAction core.InstanceActionActionEnum
	switch strings.ToUpper(action) {
	case "START":
		ociAction = core.InstanceActionActionStart
	case "STOP":
		ociAction = core.InstanceActionActionSoftstop
	case "SOFTRESET":
		ociAction = core.InstanceActionActionSoftreset
	case "RESET":
		ociAction = core.InstanceActionActionReset
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}

	req := core.InstanceActionRequest{
		InstanceId: common.String(instanceOCID),
		Action:     ociAction,
	}

	_, err = computeClient.InstanceAction(ctx, req)
	return err
}

// TerminateInstance terminates an instance
func TerminateInstance(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	req := core.TerminateInstanceRequest{
		InstanceId:         common.String(instanceOCID),
		PreserveBootVolume: common.Bool(false),
	}

	_, err = computeClient.TerminateInstance(ctx, req)
	return err
}

// ResizeInstance stops, updates shape-config, and starts instance
func ResizeInstance(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string, newOCPU, newMem float32) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	// Update instance shape config
	updateReq := core.UpdateInstanceRequest{
		InstanceId: common.String(instanceOCID),
		UpdateInstanceDetails: core.UpdateInstanceDetails{
			ShapeConfig: &core.UpdateInstanceShapeConfigDetails{
				Ocpus:       common.Float32(newOCPU),
				MemoryInGBs: common.Float32(newMem),
			},
		},
	}

	_, err = computeClient.UpdateInstance(ctx, updateReq)
	return err
}

// UpdateInstanceTags updates freeform tags on Oracle Cloud
func UpdateInstanceTags(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string, tags map[string]string) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	updateReq := core.UpdateInstanceRequest{
		InstanceId: common.String(instanceOCID),
		UpdateInstanceDetails: core.UpdateInstanceDetails{
			FreeformTags: tags,
		},
	}

	_, err = computeClient.UpdateInstance(ctx, updateReq)
	return err
}

// RotatePublicIP rotates the public IPv4 of an instance
func RotatePublicIP(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (string, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return "", err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", err
	}

	// 1. Get primary VNIC
	vnicList, err := computeClient.ListVnicAttachments(ctx, core.ListVnicAttachmentsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		InstanceId:    common.String(instanceOCID),
	})
	if err != nil || len(vnicList.Items) == 0 {
		return "", fmt.Errorf("unable to find VNIC for instance: %w", err)
	}
	vnicID := vnicList.Items[0].VnicId

	// 2. Get Private IP OCID
	vnicResp, err := netClient.GetVnic(ctx, core.GetVnicRequest{VnicId: vnicID})
	if err != nil {
		return "", fmt.Errorf("unable to get VNIC detail: %w", err)
	}

	privIPList, err := netClient.ListPrivateIps(ctx, core.ListPrivateIpsRequest{
		VnicId: vnicID,
	})
	if err != nil || len(privIPList.Items) == 0 {
		return "", fmt.Errorf("unable to find private IP: %w", err)
	}
	privIPID := privIPList.Items[0].Id

	// 3. Find and delete existing Ephemeral Public IP
	pubIPList, err := netClient.ListPublicIps(ctx, core.ListPublicIpsRequest{
		Scope:         core.ListPublicIpsScopeRegion,
		CompartmentId: common.String(profile.TenancyOCID),
	})
	if err == nil {
		for _, pub := range pubIPList.Items {
			if pub.AssignedEntityId != nil && *pub.AssignedEntityId == *privIPID {
				_, _ = netClient.DeletePublicIp(ctx, core.DeletePublicIpRequest{PublicIpId: pub.Id})
				time.Sleep(3 * time.Second)
				break
			}
		}
	}

	// 4. Create new Ephemeral Public IP
	createReq := core.CreatePublicIpRequest{
		CreatePublicIpDetails: core.CreatePublicIpDetails{
			CompartmentId: common.String(profile.TenancyOCID),
			Lifetime:      core.CreatePublicIpDetailsLifetimeEphemeral,
			PrivateIpId:   privIPID,
		},
	}
	createResp, err := netClient.CreatePublicIp(ctx, createReq)
	if err != nil {
		return "", fmt.Errorf("failed to create new public IP: %w", err)
	}

	return StrVal(createResp.PublicIp.IpAddress), nil
}

// AttachIPv6ToInstance attaches an IPv6 address to an existing instance
func AttachIPv6ToInstance(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (string, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return "", err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", err
	}

	vnicList, err := computeClient.ListVnicAttachments(ctx, core.ListVnicAttachmentsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		InstanceId:    common.String(instanceOCID),
	})
	if err != nil || len(vnicList.Items) == 0 {
		return "", fmt.Errorf("unable to find VNIC for instance: %w", err)
	}
	vnicID := vnicList.Items[0].VnicId

	// Create IPv6 on VNIC
	createReq := core.CreateIpv6Request{
		CreateIpv6Details: core.CreateIpv6Details{
			VnicId: vnicID,
		},
	}
	resp, err := netClient.CreateIpv6(ctx, createReq)
	if err != nil {
		return "", fmt.Errorf("failed to allocate IPv6: %w", err)
	}

	return StrVal(resp.Ipv6.IpAddress), nil
}

// ProbeIPPort tests TCP connection on port
func ProbeIPPort(ip string, port int, timeout time.Duration) bool {
	target := fmt.Sprintf("%s:%d", ip, port)
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
