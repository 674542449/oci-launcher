package oci

import (
	"context"
	"strings"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/core"
)

// Capacity report statuses as returned by OCI.
const (
	CapacityAvailable    = "AVAILABLE"
	CapacityOutOfHost    = "OUT_OF_HOST_CAPACITY"
	CapacityNotSupported = "HARDWARE_NOT_SUPPORTED"
)

// ADCapacity is the capacity report answer for one availability domain.
type ADCapacity struct {
	AD        string `json:"ad"`
	Status    string `json:"status"`
	Available int64  `json:"available"`
}

// CheckCapacity asks the Compute Capacity Report whether the shape (with its OCPU / memory
// configuration for flex shapes) can currently be created in one availability domain. It is a
// read-only call that Oracle documents as the way to check before creating an instance.
func CheckCapacity(ctx context.Context, profile *storage.OCIProfile, region, ad, shape string, ocpu, memoryGB float64) (ADCapacity, error) {
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return ADCapacity{}, err
	}

	want := core.CreateCapacityReportShapeAvailabilityDetails{InstanceShape: common.String(shape)}
	if strings.Contains(shape, "Flex") {
		want.InstanceShapeConfig = &core.CapacityReportInstanceShapeConfig{
			Ocpus:       common.Float32(float32(ocpu)),
			MemoryInGBs: common.Float32(float32(memoryGB)),
		}
	}

	resp, err := computeClient.CreateComputeCapacityReport(ctx, core.CreateComputeCapacityReportRequest{
		CreateComputeCapacityReportDetails: core.CreateComputeCapacityReportDetails{
			CompartmentId:       common.String(profile.TenancyOCID),
			AvailabilityDomain:  common.String(ad),
			ShapeAvailabilities: []core.CreateCapacityReportShapeAvailabilityDetails{want},
		},
	})
	if err != nil {
		return ADCapacity{}, err
	}

	out := ADCapacity{AD: ad, Status: CapacityOutOfHost}
	for _, s := range resp.ComputeCapacityReport.ShapeAvailabilities {
		out.Status = string(s.AvailabilityStatus)
		out.Available = Int64Val(s.AvailableCount)
		break
	}
	return out, nil
}

// CheckCapacityAcrossADs runs the report for every availability domain in order.
func CheckCapacityAcrossADs(ctx context.Context, profile *storage.OCIProfile, region string, ads []string, shape string, ocpu, memoryGB float64) ([]ADCapacity, error) {
	out := make([]ADCapacity, 0, len(ads))
	for _, ad := range ads {
		c, err := CheckCapacity(ctx, profile, region, ad, shape, ocpu, memoryGB)
		if err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, nil
}

// AvailableADs returns the availability domains the report marked AVAILABLE, in order.
func AvailableADs(reports []ADCapacity) []string {
	var ads []string
	for _, r := range reports {
		if r.Status == CapacityAvailable {
			ads = append(ads, r.AD)
		}
	}
	return ads
}

// IsCapacityReportUnavailable tells whether the error means the tenancy or user cannot use
// the capacity report at all (permission, unsupported region), as opposed to a passing fault.
func IsCapacityReportUnavailable(err error) bool {
	status, _, isSvc := ServiceErrorInfo(err)
	if !isSvc {
		return false
	}
	switch status {
	case 400, 401, 403, 404, 405, 501:
		return true
	}
	return false
}

// SummarizeCapacity renders the report as one short line for task messages and logs.
func SummarizeCapacity(reports []ADCapacity) string {
	parts := make([]string, 0, len(reports))
	for _, r := range reports {
		label := "无容量"
		switch r.Status {
		case CapacityAvailable:
			label = "有容量"
		case CapacityNotSupported:
			label = "不支持该规格"
		}
		parts = append(parts, shortADName(r.AD)+" "+label)
	}
	return strings.Join(parts, "，")
}

func shortADName(ad string) string {
	if i := strings.Index(ad, ":"); i >= 0 {
		return ad[i+1:]
	}
	return ad
}
