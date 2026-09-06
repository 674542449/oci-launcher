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

const (
	metricsWindowDays = 7
	// reclaimThreshold is Oracle's Always Free idle rule: an instance whose CPU 95th percentile,
	// memory and network usage all stay under 20 % for 7 days may be reclaimed.
	reclaimThreshold = 20.0
)

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
	Threshold     float64      `json:"threshold"`
	DataAvailable bool         `json:"data_available"`
	CPU           MetricSeries `json:"cpu"`     // percent
	Memory        MetricSeries `json:"memory"`  // percent (empty when the agent does not report it)
	NetIn         MetricSeries `json:"net_in"`  // bytes per hour
	NetOut        MetricSeries `json:"net_out"` // bytes per hour
	NetTotalBytes float64      `json:"net_total_bytes"`
	IdleRisk      string       `json:"idle_risk"` // "high" | "low" | "unknown"
	IdleDays      int          `json:"idle_days"` // consecutive most-recent days under the reclaim line
	Note          string       `json:"note"`
}

// GetInstanceMetrics reads 7 days of hourly compute-agent metrics for one instance and applies
// Oracle's idle-reclaim rule to them. Network has no percentage metric, so the verdict rests on
// CPU (95th percentile) and memory; network volume is reported for context.
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
	result := &InstanceMetrics{InstanceOCID: instanceOCID, WindowDays: metricsWindowDays, Resolution: "1h", Threshold: reclaimThreshold}
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
		result.IdleRisk = "unknown"
		result.Note = "没有监控数据：实例内的 Oracle Cloud Agent 监控插件未启用，或实例刚创建不久"
		return result, nil
	}
	result.DataAvailable = true
	result.IdleDays = consecutiveIdleDays(result.CPU.Points, result.Memory.Points, now)

	cpuIdle := result.CPU.P95 < reclaimThreshold
	memIdle := len(result.Memory.Points) == 0 || result.Memory.Avg < reclaimThreshold
	switch {
	case cpuIdle && memIdle && result.IdleDays >= metricsWindowDays:
		result.IdleRisk = "high"
		result.Note = fmt.Sprintf("近 %d 天 CPU 95 分位 %.1f%%、内存均值 %.1f%%，都低于 20%% 回收线，Oracle 可能回收该实例", metricsWindowDays, result.CPU.P95, result.Memory.Avg)
	case cpuIdle && memIdle:
		result.IdleRisk = "low"
		result.Note = fmt.Sprintf("已连续 %d 天低于 20%% 回收线（满 7 天才会触发回收），CPU 95 分位 %.1f%%", result.IdleDays, result.CPU.P95)
	default:
		result.IdleRisk = "low"
		result.Note = fmt.Sprintf("近 %d 天有足够负载：CPU 95 分位 %.1f%%，内存均值 %.1f%%", metricsWindowDays, result.CPU.P95, result.Memory.Avg)
	}
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

// consecutiveIdleDays counts, from the most recent day backwards, how many days had a CPU
// 95th percentile and a memory mean under the reclaim line (a day without data breaks the run).
func consecutiveIdleDays(cpu, mem []MetricPoint, now time.Time) int {
	day := func(t string) string {
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return ""
		}
		return parsed.UTC().Format("2006-01-02")
	}
	cpuByDay := map[string][]float64{}
	for _, p := range cpu {
		cpuByDay[day(p.T)] = append(cpuByDay[day(p.T)], p.V)
	}
	memByDay := map[string][]float64{}
	for _, p := range mem {
		memByDay[day(p.T)] = append(memByDay[day(p.T)], p.V)
	}

	count := 0
	for d := 0; d < metricsWindowDays; d++ {
		key := now.Add(-time.Duration(d) * 24 * time.Hour).UTC().Format("2006-01-02")
		vals := cpuByDay[key]
		if len(vals) == 0 {
			if d == 0 {
				continue // today may not have a full hour yet
			}
			break
		}
		sort.Float64s(vals)
		p95 := vals[int(math.Ceil(0.95*float64(len(vals))))-1]
		memAvg := 0.0
		if mv := memByDay[key]; len(mv) > 0 {
			var sum float64
			for _, v := range mv {
				sum += v
			}
			memAvg = sum / float64(len(mv))
		}
		if p95 >= reclaimThreshold || memAvg >= reclaimThreshold {
			break
		}
		count++
	}
	return count
}
