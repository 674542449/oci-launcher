package oci

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

type InstanceItem struct {
	OCID         string            `json:"ocid"`
	Compartment  string            `json:"compartment"` // "root" or the sub-compartment name
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

// primaryVnic returns the instance's primary VNIC (the one that carries the public IP).
// Only ATTACHED attachments are considered; the primary VNIC is preferred over secondaries.
func primaryVnic(ctx context.Context, computeClient core.ComputeClient, netClient core.VirtualNetworkClient, tenancyOCID, instanceOCID string) (core.Vnic, error) {
	req := core.ListVnicAttachmentsRequest{
		CompartmentId: common.String(tenancyOCID),
		InstanceId:    common.String(instanceOCID),
	}
	var fallback *core.Vnic
	for {
		resp, err := computeClient.ListVnicAttachments(ctx, req)
		if err != nil {
			return core.Vnic{}, err
		}
		for _, va := range resp.Items {
			if va.LifecycleState != core.VnicAttachmentLifecycleStateAttached || va.VnicId == nil {
				continue
			}
			vnicResp, err := netClient.GetVnic(ctx, core.GetVnicRequest{VnicId: va.VnicId})
			if err != nil {
				continue
			}
			if BoolVal(vnicResp.Vnic.IsPrimary) {
				return vnicResp.Vnic, nil
			}
			if fallback == nil {
				v := vnicResp.Vnic
				fallback = &v
			}
		}
		if resp.OpcNextPage == nil {
			break
		}
		req.Page = resp.OpcNextPage
	}
	if fallback != nil {
		return *fallback, nil
	}
	return core.Vnic{}, fmt.Errorf("no attached VNIC found for instance %s", instanceOCID)
}

// instanceCompartment returns the compartment an instance lives in. VNIC attachments are listed
// per compartment, so the tenancy root would miss instances created in a sub-compartment.
func instanceCompartment(ctx context.Context, computeClient core.ComputeClient, profile *storage.OCIProfile, instanceOCID string) string {
	resp, err := computeClient.GetInstance(ctx, core.GetInstanceRequest{InstanceId: common.String(instanceOCID)})
	if err != nil || resp.Instance.CompartmentId == nil || *resp.Instance.CompartmentId == "" {
		return profile.TenancyOCID
	}
	return *resp.Instance.CompartmentId
}

// ListInstancesWithDetails retrieves instances (all pages, all compartments) with primary VNIC details and the root password tag
func ListInstancesWithDetails(ctx context.Context, profile *storage.OCIProfile, region string) ([]InstanceItem, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return nil, err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return nil, err
	}

	// Instances created from the OCI console often live in a sub-compartment: cover them all.
	type located struct {
		inst core.Instance
		comp Compartment
	}
	var instances []located
comps:
	for _, comp := range ListCompartments(ctx, profile) {
		req := core.ListInstancesRequest{
			CompartmentId: common.String(comp.ID),
			SortBy:        core.ListInstancesSortByTimecreated,
			SortOrder:     core.ListInstancesSortOrderDesc,
			Limit:         common.Int(100),
		}
		for {
			resp, err := computeClient.ListInstances(ctx, req)
			if err != nil {
				if skipUnreadableCompartment(comp, profile, err) {
					continue comps
				}
				return nil, err
			}
			for _, inst := range resp.Items {
				instances = append(instances, located{inst: inst, comp: comp})
			}
			if resp.OpcNextPage == nil {
				break
			}
			req.Page = resp.OpcNextPage
		}
	}
	sort.SliceStable(instances, func(i, j int) bool {
		ti, tj := instances[i].inst.TimeCreated, instances[j].inst.TimeCreated
		if ti == nil || tj == nil {
			return ti != nil
		}
		return ti.Time.After(tj.Time)
	})

	items := make([]InstanceItem, 0, len(instances))
	for _, li := range instances {
		inst := li.inst
		if inst.LifecycleState == core.InstanceLifecycleStateTerminated {
			continue
		}

		item := InstanceItem{
			Compartment:  li.comp.Name,
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
			item.TimeCreated = inst.TimeCreated.Format(time.RFC3339)
		}
		if inst.FreeformTags != nil {
			if pass, ok := inst.FreeformTags["root_password"]; ok {
				item.RootPassword = pass
			}
		}

		// Terminating instances have no usable VNIC; skip the extra calls.
		if inst.LifecycleState != core.InstanceLifecycleStateTerminating && inst.Id != nil {
			if vnic, err := primaryVnic(ctx, computeClient, netClient, li.comp.ID, *inst.Id); err == nil {
				item.PublicIP = StrVal(vnic.PublicIp)
				item.PrivateIP = StrVal(vnic.PrivateIp)
				if len(vnic.Ipv6Addresses) > 0 {
					item.IPv6 = vnic.Ipv6Addresses[0]
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

// shellSingleQuote quotes s for safe use inside single quotes in a POSIX shell.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BuildCloudInitUserData generates the base64 cloud-init user_data script.
//
// Ubuntu 22.04/24.04 cloud images ship /etc/ssh/sshd_config.d/60-cloudimg-settings.conf with
// "PasswordAuthentication no", and sshd honours the FIRST occurrence of a keyword. Appending to
// that file therefore has no effect; a lexically earlier drop-in (00-*) is the reliable way.
func BuildCloudInitUserData(loginMode, sshKey, rootPassword string, enableBBR bool) string {
	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString("exec >>/var/log/oci_init.log 2>&1\n")
	script.WriteString("echo '=== Initializing OCI Instance ==='\n")

	// 1. Flush the in-guest firewalls (OCI images ship iptables/ip6tables rules that reject everything but SSH)
	script.WriteString("for fw in iptables ip6tables; do\n")
	script.WriteString("  $fw -P INPUT ACCEPT || true; $fw -P FORWARD ACCEPT || true; $fw -P OUTPUT ACCEPT || true\n")
	script.WriteString("  $fw -F || true; $fw -X || true\n")
	script.WriteString("done\n")
	script.WriteString("netfilter-persistent save || true\n")
	script.WriteString("ufw disable || true\n")
	script.WriteString("systemctl stop firewalld || true\n")
	script.WriteString("systemctl disable firewalld || true\n")

	// 2. SSH configuration
	script.WriteString("mkdir -p /root/.ssh && chmod 700 /root/.ssh\n")
	script.WriteString("mkdir -p /etc/ssh/sshd_config.d\n")

	if loginMode == "root_password" && rootPassword != "" {
		script.WriteString("printf '%s\\n' " + shellSingleQuote("root:"+rootPassword) + " | chpasswd\n")
		script.WriteString("cat > /etc/ssh/sshd_config.d/00-oci-panel.conf <<'EOF'\n")
		script.WriteString("PermitRootLogin yes\nPasswordAuthentication yes\nKbdInteractiveAuthentication yes\n")
		script.WriteString("EOF\n")
		// Older images without an Include line
		script.WriteString("sed -i 's/^#*PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config\n")
		script.WriteString("sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config\n")
	} else if sshKey != "" {
		script.WriteString("printf '%s\\n' " + shellSingleQuote(strings.TrimSpace(sshKey)) + " >> /root/.ssh/authorized_keys\n")
		script.WriteString("chmod 600 /root/.ssh/authorized_keys\n")
		script.WriteString("cat > /etc/ssh/sshd_config.d/00-oci-panel.conf <<'EOF'\n")
		script.WriteString("PermitRootLogin prohibit-password\n")
		script.WriteString("EOF\n")
		script.WriteString("sed -i 's/^#*PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config\n")
	}
	// Ubuntu's default authorized_keys for root carries a "no-port-forwarding ... command=..." prefix that blocks root logins
	script.WriteString("sed -i 's/^no-port-forwarding.*sleep 10\" //' /root/.ssh/authorized_keys 2>/dev/null || true\n")
	script.WriteString("systemctl restart ssh || systemctl restart sshd || true\n")

	// 3. BBR
	if enableBBR {
		script.WriteString("grep -q 'tcp_congestion_control=bbr' /etc/sysctl.conf || printf '%s\\n' 'net.core.default_qdisc=fq' 'net.ipv4.tcp_congestion_control=bbr' >> /etc/sysctl.conf\n")
		script.WriteString("sysctl -p || true\n")
	}

	return base64.StdEncoding.EncodeToString([]byte(script.String()))
}

// normalizeVPU snaps a requested VPU/GB value to what the API accepts: 10, 20, 30…120.
func normalizeVPU(vpu int64, allowZero bool) int64 {
	if vpu <= 0 {
		if allowZero {
			return 0
		}
		return 10
	}
	if vpu < 10 {
		return 10
	}
	if vpu > 120 {
		return 120
	}
	return (vpu / 10) * 10
}

// LaunchInstance launches a compute instance with cloud-init, boot volume settings and tags
func LaunchInstance(ctx context.Context, profile *storage.OCIProfile, task *storage.LaunchTask, targetAD string) (string, error) {
	computeClient, err := GetComputeClient(profile, task.Region)
	if err != nil {
		return "", err
	}

	// Only the root password tag (a documented feature of this panel); no tool fingerprint.
	tags := map[string]string{}
	if task.LoginMode == "root_password" && task.RootPasswordEnc != "" {
		tags["root_password"] = task.RootPasswordEnc
	}

	userDataB64 := BuildCloudInitUserData(task.LoginMode, task.SSHAuthorizedKeys, task.RootPasswordEnc, true)

	vpu := normalizeVPU(task.BootVolumeVPU, false)
	if task.BootVolumeVPU <= 0 {
		vpu = 120 // legacy default of this panel
	}
	bootSize := task.BootVolumeSizeInGBs
	if bootSize < 50 {
		bootSize = 50 // API minimum when a size is specified explicitly
	}

	details := core.LaunchInstanceDetails{
		CompartmentId:      common.String(profile.TenancyOCID),
		AvailabilityDomain: common.String(targetAD),
		DisplayName:        common.String(task.InstanceName),
		Shape:              common.String(task.Shape),
		SourceDetails: core.InstanceSourceViaImageDetails{
			ImageId:             common.String(task.ImageOCID),
			BootVolumeSizeInGBs: common.Int64(bootSize),
			BootVolumeVpusPerGB: common.Int64(vpu),
		},
		CreateVnicDetails: &core.CreateVnicDetails{
			SubnetId:       common.String(task.SubnetOCID),
			AssignPublicIp: common.Bool(task.AssignPublicIP),
			AssignIpv6Ip:   common.Bool(task.EnableIPv6),
			DisplayName:    common.String(task.InstanceName),
		},
		Metadata: map[string]string{
			"user_data": userDataB64,
		},
	}
	if len(tags) > 0 {
		details.FreeformTags = tags
	}
	if task.LoginMode != "root_password" && strings.TrimSpace(task.SSHAuthorizedKeys) != "" {
		// Also register the key through the platform so the default user works even if cloud-init fails
		details.Metadata["ssh_authorized_keys"] = strings.TrimSpace(task.SSHAuthorizedKeys)
	}

	if strings.Contains(task.Shape, "Flex") {
		details.ShapeConfig = &core.LaunchInstanceShapeConfigDetails{
			Ocpus:       common.Float32(float32(task.OCPU)),
			MemoryInGBs: common.Float32(float32(task.MemoryInGBs)),
		}
	}

	// The SDK stamps one opc-retry-token on this call, so the in-call retries below can never
	// create a second instance: a retried request that already went through returns the same one.
	policy := launchRetryPolicy()
	resp, err := computeClient.LaunchInstance(ctx, core.LaunchInstanceRequest{
		LaunchInstanceDetails: details,
		RequestMetadata:       common.RequestMetadata{RetryPolicy: &policy},
	})
	if err != nil {
		return "", err
	}

	return StrVal(resp.Instance.Id), nil
}

// launchRetryPolicy retries a single LaunchInstance call (up to 3 attempts, 3 s / 6 s apart)
// on network errors, timeouts and 5xx answers whose outcome is unknown. Capacity (500 "out of
// host capacity") and 429 are deliberately not retried here: the engine rotates availability
// domains and backs off for those.
func launchRetryPolicy() common.RetryPolicy {
	shouldRetry := func(r common.OCIOperationResponse) bool {
		if r.Error == nil {
			return false
		}
		if errors.Is(r.Error, context.Canceled) || errors.Is(r.Error, context.DeadlineExceeded) {
			return false
		}
		if se, ok := common.IsServiceError(r.Error); ok {
			code := se.GetHTTPStatusCode()
			switch {
			case code == 429, code == 501:
				return false
			case code == 500 && IsCapacityMessage(se.GetMessage()):
				return false
			case code >= 500:
				return true
			case code == 409 && se.GetCode() == "IncorrectState":
				return true
			}
			return false
		}
		return true // transport-level failure: safe to retry with the same token
	}
	next := func(r common.OCIOperationResponse) time.Duration {
		return time.Duration(3*int(r.AttemptNumber)) * time.Second
	}
	return common.NewRetryPolicy(3, shouldRetry, next)
}

// IsCapacityMessage tells whether an OCI error message is the free-tier capacity answer.
func IsCapacityMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "out of host capacity") || strings.Contains(lower, "out of capacity")
}

// InstanceAction performs START, STOP (graceful), SOFTRESET, RESET
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

	_, err = computeClient.InstanceAction(ctx, core.InstanceActionRequest{
		InstanceId: common.String(instanceOCID),
		Action:     ociAction,
	})
	return err
}

// TerminateInstance terminates an instance and its boot volume
func TerminateInstance(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	_, err = computeClient.TerminateInstance(ctx, core.TerminateInstanceRequest{
		InstanceId:         common.String(instanceOCID),
		PreserveBootVolume: common.Bool(false),
	})
	return err
}

// ResizeInstance updates the flexible shape configuration (the service reboots the instance)
func ResizeInstance(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string, newOCPU, newMem float32) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	_, err = computeClient.UpdateInstance(ctx, core.UpdateInstanceRequest{
		InstanceId: common.String(instanceOCID),
		UpdateInstanceDetails: core.UpdateInstanceDetails{
			ShapeConfig: &core.UpdateInstanceShapeConfigDetails{
				Ocpus:       common.Float32(newOCPU),
				MemoryInGBs: common.Float32(newMem),
			},
		},
	})
	return err
}

// UpdateInstanceTags replaces the instance's freeform tags (the caller merges with existing tags)
func UpdateInstanceTags(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string, tags map[string]string) error {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return err
	}

	_, err = computeClient.UpdateInstance(ctx, core.UpdateInstanceRequest{
		InstanceId: common.String(instanceOCID),
		UpdateInstanceDetails: core.UpdateInstanceDetails{
			FreeformTags: tags,
		},
	})
	return err
}

// RotatePublicIP replaces the ephemeral public IPv4 of an instance's primary VNIC.
//
// Ephemeral public IPs are AD-scoped objects that can only be attached to the PRIMARY private IP
// of a VNIC, and a private IP may hold at most one public IP. So: look the current public IP up
// through the private IP, delete it if it is ephemeral (or merely unassign it if it is reserved,
// which must never be destroyed), wait until it is gone, then create a new ephemeral one.
func RotatePublicIP(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (string, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return "", err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", err
	}

	vnic, err := primaryVnic(ctx, computeClient, netClient, instanceCompartment(ctx, computeClient, profile, instanceOCID), instanceOCID)
	if err != nil {
		return "", fmt.Errorf("unable to find VNIC for instance: %w", err)
	}

	// Primary private IP of the primary VNIC
	privResp, err := netClient.ListPrivateIps(ctx, core.ListPrivateIpsRequest{VnicId: vnic.Id})
	if err != nil {
		return "", fmt.Errorf("unable to list private IPs: %w", err)
	}
	var privIPID *string
	for _, p := range privResp.Items {
		if BoolVal(p.IsPrimary) && p.Id != nil {
			privIPID = p.Id
			break
		}
	}
	if privIPID == nil && len(privResp.Items) > 0 {
		privIPID = privResp.Items[0].Id
	}
	if privIPID == nil {
		return "", fmt.Errorf("unable to find the primary private IP of the VNIC")
	}

	// Current public IP (404 means none assigned)
	pubResp, err := netClient.GetPublicIpByPrivateIpId(ctx, core.GetPublicIpByPrivateIpIdRequest{
		GetPublicIpByPrivateIpIdDetails: core.GetPublicIpByPrivateIpIdDetails{PrivateIpId: privIPID},
	})
	if err != nil && !IsNotFoundError(err) {
		return "", fmt.Errorf("unable to look up current public IP: %w", err)
	}
	if err == nil && pubResp.PublicIp.Id != nil {
		switch pubResp.PublicIp.Lifetime {
		case core.PublicIpLifetimeReserved:
			// Keep the reserved address; just detach it so a fresh ephemeral one can be assigned.
			if _, err := netClient.UpdatePublicIp(ctx, core.UpdatePublicIpRequest{
				PublicIpId:            pubResp.PublicIp.Id,
				UpdatePublicIpDetails: core.UpdatePublicIpDetails{PrivateIpId: common.String("")},
			}); err != nil {
				return "", fmt.Errorf("unable to unassign reserved public IP: %w", err)
			}
		default:
			if _, err := netClient.DeletePublicIp(ctx, core.DeletePublicIpRequest{PublicIpId: pubResp.PublicIp.Id}); err != nil && !IsNotFoundError(err) {
				return "", fmt.Errorf("unable to release current public IP: %w", err)
			}
		}
		// Wait until the old public IP is really gone / detached
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			cur, err := netClient.GetPublicIp(ctx, core.GetPublicIpRequest{PublicIpId: pubResp.PublicIp.Id})
			if IsNotFoundError(err) || (err == nil && (cur.PublicIp.LifecycleState == core.PublicIpLifecycleStateTerminated || cur.PublicIp.LifecycleState == core.PublicIpLifecycleStateAvailable)) {
				break
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}

	// New ephemeral public IP
	createResp, err := netClient.CreatePublicIp(ctx, core.CreatePublicIpRequest{
		CreatePublicIpDetails: core.CreatePublicIpDetails{
			CompartmentId: common.String(profile.TenancyOCID),
			Lifetime:      core.CreatePublicIpDetailsLifetimeEphemeral,
			PrivateIpId:   privIPID,
			DisplayName:   common.String(StrVal(vnic.DisplayName) + "-public-ip"),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to create new public IP: %w", err)
	}

	newIP := StrVal(createResp.PublicIp.IpAddress)
	if newIP == "" && createResp.PublicIp.Id != nil {
		// The address can take a moment to be allocated
		for i := 0; i < 10 && newIP == ""; i++ {
			time.Sleep(2 * time.Second)
			cur, err := netClient.GetPublicIp(ctx, core.GetPublicIpRequest{PublicIpId: createResp.PublicIp.Id})
			if err == nil {
				newIP = StrVal(cur.PublicIp.IpAddress)
			}
		}
	}
	return newIP, nil
}

// AttachIPv6ToInstance allocates an IPv6 address on the instance's primary VNIC.
// The subnet must have an IPv6 prefix, otherwise the API returns 400.
func AttachIPv6ToInstance(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (string, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return "", err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", err
	}

	vnic, err := primaryVnic(ctx, computeClient, netClient, instanceCompartment(ctx, computeClient, profile, instanceOCID), instanceOCID)
	if err != nil {
		return "", fmt.Errorf("unable to find VNIC for instance: %w", err)
	}
	if len(vnic.Ipv6Addresses) > 0 {
		return vnic.Ipv6Addresses[0], nil
	}

	resp, err := netClient.CreateIpv6(ctx, core.CreateIpv6Request{
		CreateIpv6Details: core.CreateIpv6Details{VnicId: vnic.Id},
	})
	if err != nil {
		return "", fmt.Errorf("failed to allocate IPv6 (the subnet must be IPv6-enabled): %w", err)
	}

	return StrVal(resp.Ipv6.IpAddress), nil
}

// GetPrimaryVnicAddresses returns the primary VNIC's public IPv4 and first IPv6 address.
func GetPrimaryVnicAddresses(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (pubIP, ipv6 string, err error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return "", "", err
	}
	netClient, err := GetVirtualNetworkClient(profile, region)
	if err != nil {
		return "", "", err
	}
	vnic, err := primaryVnic(ctx, computeClient, netClient, instanceCompartment(ctx, computeClient, profile, instanceOCID), instanceOCID)
	if err != nil {
		return "", "", err
	}
	pubIP = StrVal(vnic.PublicIp)
	if len(vnic.Ipv6Addresses) > 0 {
		ipv6 = vnic.Ipv6Addresses[0]
	}
	return pubIP, ipv6, nil
}

// ProbeIPPort tests a TCP connection to ip:port
func ProbeIPPort(ip string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
