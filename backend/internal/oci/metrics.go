package oci

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/monitoring"
)

const metricsWindowDays = 7

type MetricPoint struct {
	T string  `json:"t"` // RFC3339 UTC
	V float64 `json:"v"`
}

type MetricSeries struct {
	Points []MetricPoint `json:"points"`
	Avg    float64       `json:"avg"`
	Max    float64       `json:"max"`
	P95    float64       `json:"p95"`
}

type InstanceMetrics struct {
	InstanceOCID  string       `json:"instance_ocid"`
	WindowDays    int          `json:"window_days"`
	Resolution    string       `json:"resolution"`
	DataAvailable bool         `json:"data_available"`
	CPU           MetricSeries `json:"cpu"`     // percent
	Memory        MetricSeries `json:"memory"`  // percent (empty when the agent does not report it)
	NetIn         MetricSeries `json:"net_in"`  // bytes per hour
	NetOut        MetricSeries `json:"net_out"` // bytes per hour
	NetTotalBytes float64      `json:"net_total_bytes"`
	Note          string       `json:"note"` // only set when there is nothing to show
}

// GetInstanceMetrics reads 7 days of hourly compute-agent metrics for one instance: CPU and
// memory utilization (percent) and network volume.
//
// NetworksBytesIn/Out are cumulative counters (sampled every 10 s, reset when the OS restarts),
// so they are read with increment(): the change per hour is the bytes moved in that hour. A
// counter reset makes one interval negative, which is clamped to zero.
func GetInstanceMetrics(ctx context.Context, profile *storage.OCIProfile, region, instanceOCID string) (*InstanceMetrics, error) {
	monClient, err := GetMonitoringClient(profile, region)
	if err != nil {
		return nil, err
	}
	computeClient, err := GetComputeClient(profile, region)
	if err != nil {
		return nil, err
	}
	compartment := instanceCompartment(ctx, computeClient, profile, instanceOCID)

	now := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
	start := now.Add(-metricsWindowDays * 24 * time.Hour)

	query := func(metric, fn string) (MetricSeries, error) {
		resp, err := monClient.SummarizeMetricsData(ctx, monitoring.SummarizeMetricsDataRequest{
			CompartmentId: common.String(compartment),
			SummarizeMetricsDataDetails: monitoring.SummarizeMetricsDataDetails{
				Namespace:  common.String("oci_computeagent"),
				Query:      common.String(fmt.Sprintf("%s[1h]{resourceId = \"%s\"}.%s()", metric, instanceOCID, fn)),
				StartTime:  &common.SDKTime{Time: start},
				EndTime:    &common.SDKTime{Time: now},
				Resolution: common.String("1h"),
			},
		})
		if err != nil {
			return MetricSeries{}, err
		}
		var pts []MetricPoint
		for _, item := range resp.Items {
			for _, dp := range item.AggregatedDatapoints {
				if dp.Value == nil || dp.Timestamp == nil {
					continue
				}
				pts = append(pts, MetricPoint{T: dp.Timestamp.UTC().Format(time.RFC3339), V: math.Max(0, *dp.Value)})
			}
		}
		sort.Slice(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
		return summarize(pts), nil
	}

	type job struct {
		metric, fn string
		out        *MetricSeries
		err        error
	}
	result := &InstanceMetrics{InstanceOCID: instanceOCID, WindowDays: metricsWindowDays, Resolution: "1h"}
	jobs := []*job{
		{metric: "CpuUtilization", fn: "mean", out: &result.CPU},
		{metric: "MemoryUtilization", fn: "mean", out: &result.Memory},
		{metric: "NetworksBytesIn", fn: "increment", out: &result.NetIn},
		{metric: "NetworksBytesOut", fn: "increment", out: &result.NetOut},
	}
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j *job) {
			defer wg.Done()
			s, err := query(j.metric, j.fn)
			if err != nil {
				j.err = err
				return
			}
			*j.out = s
		}(j)
	}
	wg.Wait()
	if jobs[0].err != nil {
		return nil, fmt.Errorf("monitoring query failed: %w", jobs[0].err)
	}

	for _, p := range result.NetIn.Points {
		result.NetTotalBytes += p.V
	}
	for _, p := range result.NetOut.Points {
		result.NetTotalBytes += p.V
	}

	if len(result.CPU.Points) == 0 {
		result.Note = "暂无监控数据：实例内 Oracle Cloud Agent 监控插件未启用，或实例创建时间过短"
		return result, nil
	}
	result.DataAvailable = true
	return result, nil
}

func summarize(pts []MetricPoint) MetricSeries {
	s := MetricSeries{Points: pts}
	if len(pts) == 0 {
		return s
	}
	vals := make([]float64, len(pts))
	var sum float64
	for i, p := range pts {
		vals[i] = p.V
		sum += p.V
		if p.V > s.Max {
			s.Max = p.V
		}
	}
	s.Avg = sum / float64(len(pts))
	sort.Float64s(vals)
	idx := int(math.Ceil(0.95*float64(len(vals)))) - 1
	if idx < 0 {
		idx = 0
	}
	s.P95 = vals[idx]
	return s
}
