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
	MaxTB            float64 `json:"max_tb"` // 10.0 TB Always Free
	UsedPercent      float64 `json:"used_percent"`
	AlertLevel       string  `json:"alert_level"` // "normal", "warning" (>=80%), "critical" (>=95%)
	AlertDescription string  `json:"alert_description"`
}

// GetMonthlyOutboundTraffic queries OCI Monitoring API for BytesOut
func GetMonthlyOutboundTraffic(ctx context.Context, profile *storage.OCIProfile, region string) (*OutboundTrafficSummary, error) {
	monClient, err := GetMonitoringClient(profile, region)
	if err != nil {
		return nil, err
	}

	// Calculate start of current natural month
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

	summary := &OutboundTrafficSummary{
		MaxTB:      10.0,
		AlertLevel: "normal",
	}

	resp, err := monClient.SummarizeMetricsData(ctx, req)
	if err == nil && len(resp.Items) > 0 {
		var totalBytes float64
		for _, item := range resp.Items {
			for _, dp := range item.AggregatedDatapoints {
				if dp.Value != nil {
					totalBytes += *dp.Value
				}
			}
		}
		summary.UsedBytes = totalBytes
		summary.UsedTB = totalBytes / (1024 * 1024 * 1024 * 1024)
	}

	summary.UsedPercent = (summary.UsedTB / summary.MaxTB) * 100.0
	if summary.UsedPercent >= 95.0 {
		summary.AlertLevel = "critical"
		summary.AlertDescription = fmt.Sprintf("【高危超额警报】当月出站流量已达 %0.2f TB (%0.1f%%)，极为接近 10 TB 免费红线！请立即控制流量避免扣费！", summary.UsedTB, summary.UsedPercent)
	} else if summary.UsedPercent >= 80.0 {
		summary.AlertLevel = "warning"
		summary.AlertDescription = fmt.Sprintf("【流量预警】当月出站流量已达 %0.2f TB (%0.1f%%)，已超过 80%% 警戒水位。", summary.UsedTB, summary.UsedPercent)
	} else {
		summary.AlertDescription = fmt.Sprintf("当月出站流量健康: %0.2f TB / 10 TB 免费额度 (%0.1f%%)", summary.UsedTB, summary.UsedPercent)
	}

	return summary, nil
}
