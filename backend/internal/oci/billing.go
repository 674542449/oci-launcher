package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"oci-panel/internal/cache"
	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/tenantmanagercontrolplane"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

// Billing is read through the Usage API (RequestSummarizedUsages, query type COST), the same
// source the console's Cost Analysis uses: daily granularity, grouped by service, on UTC day
// boundaries. Trial credits come from the Organizations subscription's promotion records.
// Cost data is settled by Oracle with a delay of roughly one day, so today is never final.

const billingCacheTTL = 30 * time.Minute

type BillingDay struct {
	Date   string  `json:"date"` // YYYY-MM-DD (UTC)
	Amount float64 `json:"amount"`
}

type BillingService struct {
	Service     string  `json:"service"`
	MonthToDate float64 `json:"month_to_date"`
	LastMonth   float64 `json:"last_month"`
}

type BillingPromotion struct {
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	Status        string  `json:"status"`
	TimeStarted   string  `json:"time_started"`
	TimeExpired   string  `json:"time_expired"`
	IsIntentToPay bool    `json:"is_intent_to_pay"`
}

type BillingSummary struct {
	Currency       string             `json:"currency"`
	HomeRegion     string             `json:"home_region"`
	MonthStart     string             `json:"month_start"`
	MonthToDate    float64            `json:"month_to_date"`
	LastMonth      float64            `json:"last_month"`
	ProjectedMonth float64            `json:"projected_month"`
	ElapsedDays    int                `json:"elapsed_days"`
	DaysInMonth    int                `json:"days_in_month"`
	Daily          []BillingDay       `json:"daily"` // last 30 settled days
	Services       []BillingService   `json:"services"`
	Promotions     []BillingPromotion `json:"promotions"`
	DataAsOf       string             `json:"data_as_of"` // latest usage day present in the data
	FetchedAt      string             `json:"fetched_at"`
	Cached         bool               `json:"cached"`
	Note           string             `json:"note"`
}

// GetUsageapiClient returns a Usage API client bound to the tenancy's home region.
func GetUsageapiClient(profile *storage.OCIProfile, regionOverride ...string) (usageapi.UsageapiClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return usageapi.UsageapiClient{}, err
	}
	client, err := usageapi.NewUsageapiClientWithConfigurationProvider(provider)
	if err != nil {
		return usageapi.UsageapiClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

// GetBillingSummary returns the account's cost picture for the current and previous month.
// Results are cached for 30 minutes per tenancy unless force is set.
func GetBillingSummary(ctx context.Context, profile *storage.OCIProfile, force bool) (*BillingSummary, error) {
	cacheKey := "billing:" + profile.TenancyOCID
	if !force {
		if raw, err := cache.GetCachedMetadata(ctx, cacheKey); err == nil && raw != "" {
			var cached BillingSummary
			if json.Unmarshal([]byte(raw), &cached) == nil && cached.FetchedAt != "" {
				cached.Cached = true
				return &cached, nil
			}
		}
	}

	homeRegion, err := ResolveHomeRegion(ctx, profile)
	if err != nil {
		return nil, err
	}
	client, err := GetUsageapiClient(profile, homeRegion)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	lastMonthStart := monthStart.AddDate(0, -1, 0)
	nextMonthStart := monthStart.AddDate(0, 1, 0)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)

	req := usageapi.RequestSummarizedUsagesRequest{
		RequestSummarizedUsagesDetails: usageapi.RequestSummarizedUsagesDetails{
			TenantId:         common.String(profile.TenancyOCID),
			TimeUsageStarted: &common.SDKTime{Time: lastMonthStart},
			TimeUsageEnded:   &common.SDKTime{Time: end},
			Granularity:      usageapi.RequestSummarizedUsagesDetailsGranularityDaily,
			QueryType:        usageapi.RequestSummarizedUsagesDetailsQueryTypeCost,
			GroupBy:          []string{"service"},
		},
		Limit: common.Int(500),
	}

	summary := &BillingSummary{
		HomeRegion:  homeRegion,
		MonthStart:  monthStart.Format("2006-01-02"),
		DaysInMonth: nextMonthStart.AddDate(0, 0, -1).Day(),
		Daily:       []BillingDay{},
		Services:    []BillingService{},
		Promotions:  []BillingPromotion{},
		FetchedAt:   now.Format(time.RFC3339),
	}

	byDay := map[string]float64{}
	bySvc := map[string]*BillingService{}
	var latest time.Time
	for {
		resp, err := client.RequestSummarizedUsages(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("usage api: %w", err)
		}
		for _, it := range resp.UsageAggregation.Items {
			if it.TimeUsageStarted == nil {
				continue
			}
			amount := float64(f32(it.ComputedAmount))
			day := it.TimeUsageStarted.UTC()
			if it.Currency != nil && *it.Currency != "" {
				summary.Currency = *it.Currency
			}
			if day.After(latest) {
				latest = day
			}
			byDay[day.Format("2006-01-02")] += amount
			svc := StrVal(it.Service)
			if svc == "" {
				svc = "其他"
			}
			entry := bySvc[svc]
			if entry == nil {
				entry = &BillingService{Service: svc}
				bySvc[svc] = entry
			}
			if !day.Before(monthStart) {
				summary.MonthToDate += amount
				entry.MonthToDate += amount
			} else {
				summary.LastMonth += amount
				entry.LastMonth += amount
			}
		}
		if resp.OpcNextPage == nil || *resp.OpcNextPage == "" {
			break
		}
		req.Page = resp.OpcNextPage
	}

	// Daily series: the 30 days up to yesterday, zero-filled.
	for i := 30; i >= 1; i-- {
		d := end.AddDate(0, 0, -1-i)
		key := d.Format("2006-01-02")
		summary.Daily = append(summary.Daily, BillingDay{Date: key, Amount: round2(byDay[key])})
	}
	for _, s := range bySvc {
		if s.MonthToDate == 0 && s.LastMonth == 0 {
			continue
		}
		s.MonthToDate, s.LastMonth = round2(s.MonthToDate), round2(s.LastMonth)
		summary.Services = append(summary.Services, *s)
	}
	sort.Slice(summary.Services, func(i, j int) bool {
		if summary.Services[i].MonthToDate != summary.Services[j].MonthToDate {
			return summary.Services[i].MonthToDate > summary.Services[j].MonthToDate
		}
		return summary.Services[i].LastMonth > summary.Services[j].LastMonth
	})

	// Projection: settled days so far scaled to the whole month (today is never settled).
	elapsed := now.Day() - 1
	if elapsed < 1 {
		elapsed = 1
	}
	summary.ElapsedDays = elapsed
	summary.MonthToDate = round2(summary.MonthToDate)
	summary.LastMonth = round2(summary.LastMonth)
	summary.ProjectedMonth = round2(summary.MonthToDate / float64(elapsed) * float64(summary.DaysInMonth))
	if !latest.IsZero() {
		summary.DataAsOf = latest.Format("2006-01-02")
	}
	if summary.Currency == "" {
		summary.Currency = "USD"
	}
	if len(bySvc) == 0 {
		summary.Note = "Usage API 未返回计费记录：Always Free 资源不产生费用，新账号的用量数据通常在 24 小时后出现"
	}

	summary.Promotions = readPromotions(ctx, profile, homeRegion)

	if data, err := json.Marshal(summary); err == nil {
		_ = cache.CacheMetadata(ctx, cacheKey, string(data), billingCacheTTL)
	}
	return summary, nil
}

// readPromotions lists the trial / promotional credits attached to the tenancy's subscription.
func readPromotions(ctx context.Context, profile *storage.OCIProfile, homeRegion string) []BillingPromotion {
	out := []BillingPromotion{}
	client, err := GetSubscriptionClient(profile, homeRegion)
	if err != nil {
		return out
	}
	listResp, err := client.ListSubscriptions(ctx, tenantmanagercontrolplane.ListSubscriptionsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		Limit:         common.Int(20),
	})
	if err != nil {
		return out
	}
	for _, item := range listResp.Items {
		id := StrVal(item.GetId())
		if id == "" {
			continue
		}
		detail, err := client.GetSubscription(ctx, tenantmanagercontrolplane.GetSubscriptionRequest{SubscriptionId: common.String(id)})
		if err != nil {
			continue
		}
		classic, ok := detail.Subscription.(tenantmanagercontrolplane.ClassicSubscription)
		if !ok {
			continue
		}
		for _, p := range classic.Promotion {
			promo := BillingPromotion{
				Amount:        float64(f32(p.Amount)),
				Currency:      StrVal(p.CurrencyUnit),
				Status:        string(p.Status),
				IsIntentToPay: BoolVal(p.IsIntentToPay),
			}
			if p.TimeStarted != nil {
				promo.TimeStarted = p.TimeStarted.UTC().Format("2006-01-02")
			}
			if p.TimeExpired != nil {
				promo.TimeExpired = p.TimeExpired.UTC().Format("2006-01-02")
			}
			out = append(out, promo)
		}
	}
	return out
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func f32(p *float32) float64 {
	if p == nil {
		return 0
	}
	return float64(*p)
}
