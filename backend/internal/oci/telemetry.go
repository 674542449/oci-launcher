package oci

import (
	"context"
	"fmt"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
)

type OutboundTrafficSummary struct {
	UsedBytes        float64 `json:"used_bytes"`
	UsedTB           float64 `json:"used_tb"`
	MaxTB            float64 `json:"max_tb"` // 10 TB per month, Always Free
	UsedPercent      float64 `json:"used_percent"`
	AlertLevel       string  `json:"alert_level"` // "normal", "warning" (>=80%), "critical" (>=95%)
	AlertDescription string  `json:"alert_description"`
}

// GetMonthlyOutboundTraffic sums VnicToNetworkBytes (bytes leaving every VNIC) for the current
// calendar month via the Monitoring API.
//
// Notes on accuracy: the metric counts all bytes the VNICs send, including traffic that stays
// inside the VCN, so it over-estimates internet egress (conservative for a "stay free" goal).
// The 10 TB allowance is treated as decimal (1 TB = 1e12 bytes), which is also the conservative reading.
func GetMonthlyOutboundTraffic(ctx context.Context, profile *storage.OCIProfile, region string) (*OutboundTrafficSummary, error) {
	monClient, err := GetMonitoringClient(profile, region)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	req := monitoring.SummarizeMetricsDataRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		SummarizeMetricsDataDetails: monitoring.SummarizeMetricsDataDetails{
			Namespace:  common.String("oci_vcn"),
			Query:      common.String("VnicToNetworkBytes[1d].sum()"),
			StartTime:  &common.SDKTime{Time: startOfMonth},
			EndTime:    &common.SDKTime{Time: now},
			Resolution: common.String("1d"),
		},
	}

	resp, err := monClient.SummarizeMetricsData(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("monitoring query failed: %w", err)
	}

	var totalBytes float64
	for _, item := range resp.Items {
		for _, dp := range item.AggregatedDatapoints {
			if dp.Value != nil {
				totalBytes += *dp.Value
			}
		}
	}

	summary := &OutboundTrafficSummary{
		UsedBytes:  totalBytes,
		UsedTB:     totalBytes / 1e12,
		MaxTB:      10.0,
		AlertLevel: "normal",
	}
	summary.UsedPercent = (summary.UsedTB / summary.MaxTB) * 100.0

	switch {
	case summary.UsedPercent >= 95.0:
		summary.AlertLevel = "critical"
		summary.AlertDescription = fmt.Sprintf("当月出站流量已达 %0.2f TB（%0.1f%%），即将触及 10 TB 免费上限，超出部分按量计费", summary.UsedTB, summary.UsedPercent)
	case summary.UsedPercent >= 80.0:
		summary.AlertLevel = "warning"
		summary.AlertDescription = fmt.Sprintf("当月出站流量已达 %0.2f TB（%0.1f%%），超过 80%% 警戒线", summary.UsedTB, summary.UsedPercent)
	default:
		summary.AlertDescription = fmt.Sprintf("当月出站流量 %0.2f TB / 10 TB（%0.1f%%），含 VCN 内部流量的保守估算", summary.UsedTB, summary.UsedPercent)
	}

	return summary, nil
}
