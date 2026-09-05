package oci

import (
	"context"
	"fmt"
	"strings"
	"time"

	"oci-panel/internal/storage"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"github.com/oracle/oci-go-sdk/v65/tenantmanagercontrolplane"
)

// SubscriptionVerdict is what the Organizations API (tenant manager) reports about the tenancy's
// subscription. This is the account-level answer to "is this an upgraded (paid) account?".
type SubscriptionVerdict struct {
	Found        bool       `json:"found"`
	Decided      bool       `json:"decided"`
	IsPaid       bool       `json:"is_paid"`
	Tier         string     `json:"tier"`
	PaymentModel string     `json:"payment_model"`
	ProgramType  string     `json:"program_type"`
	Promotion    string     `json:"promotion"`
	IntentToPay  bool       `json:"intent_to_pay"`
	CountryCode  string     `json:"country_code"`
	StartDate    *time.Time `json:"start_date"`
	Reason       string     `json:"reason"`
}

func GetSubscriptionClient(profile *storage.OCIProfile, regionOverride ...string) (tenantmanagercontrolplane.SubscriptionClient, error) {
	provider, err := GetConfigurationProvider(profile)
	if err != nil {
		return tenantmanagercontrolplane.SubscriptionClient{}, err
	}
	client, err := tenantmanagercontrolplane.NewSubscriptionClientWithConfigurationProvider(provider)
	if err != nil {
		return tenantmanagercontrolplane.SubscriptionClient{}, err
	}
	client.HTTPClient = pooledHTTPClient
	if r := firstRegion(regionOverride); r != "" {
		client.SetRegion(r)
	}
	return client, nil
}

// DetectAccountTypeBySubscription asks the Organizations API for the tenancy's subscription and
// decides free vs. paid from the subscription itself:
//   - SubscriptionTier says whether it is a free promotion subscription or a paid one
//   - Promotion.IsIntentToPay is set once the customer upgraded (added a payment method)
//   - PaymentModel ("Pay as you go", "Monthly", ...) is the paid model when no promotion exists
func DetectAccountTypeBySubscription(ctx context.Context, profile *storage.OCIProfile, homeRegion string) (*SubscriptionVerdict, error) {
	client, err := GetSubscriptionClient(profile, homeRegion)
	if err != nil {
		return nil, err
	}

	listResp, err := client.ListSubscriptions(ctx, tenantmanagercontrolplane.ListSubscriptionsRequest{
		CompartmentId: common.String(profile.TenancyOCID),
		Limit:         common.Int(20),
	})
	if err != nil {
		return nil, err
	}

	verdict := &SubscriptionVerdict{}
	var evidence []string

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
			// Cloud (V2) subscriptions are Universal Credits contracts: always a paid account
			verdict.Found, verdict.Decided, verdict.IsPaid = true, true, true
			evidence = append(evidence, fmt.Sprintf("订阅 %s 为云订阅 (Universal Credits)", StrVal(item.GetServiceName())))
			continue
		}

		verdict.Found = true
		verdict.Tier = StrVal(classic.SubscriptionTier)
		verdict.PaymentModel = StrVal(classic.PaymentModel)
		verdict.ProgramType = StrVal(classic.ProgramType)
		verdict.CountryCode = strings.ToUpper(StrVal(classic.CustomerCountryCode))
		if classic.StartDate != nil {
			t := classic.StartDate.Time
			verdict.StartDate = &t
		} else if classic.TimeCreated != nil {
			t := classic.TimeCreated.Time
			verdict.StartDate = &t
		}

		tier := strings.ToLower(verdict.Tier)
		payModel := strings.ToLower(verdict.PaymentModel)

		promoActive := false
		for _, p := range classic.Promotion {
			state := string(p.Status)
			if BoolVal(p.IsIntentToPay) {
				verdict.IntentToPay = true
			}
			if p.Status == tenantmanagercontrolplane.PromotionStatusActive {
				promoActive = true
			}
			verdict.Promotion = state
			evidence = append(evidence, fmt.Sprintf("促销额度状态 %s, intentToPay=%v", state, BoolVal(p.IsIntentToPay)))
		}

		switch {
		case tier != "" && (strings.Contains(tier, "paid") || strings.Contains(tier, "standard") || strings.Contains(tier, "commit")):
			verdict.Decided, verdict.IsPaid = true, true
			evidence = append(evidence, "subscriptionTier="+verdict.Tier)
		case tier != "" && (strings.Contains(tier, "free") || strings.Contains(tier, "promo") || strings.Contains(tier, "trial")):
			verdict.Decided, verdict.IsPaid = true, verdict.IntentToPay
			evidence = append(evidence, "subscriptionTier="+verdict.Tier)
		case verdict.IntentToPay:
			verdict.Decided, verdict.IsPaid = true, true
		case len(classic.Promotion) > 0:
			// Promotion present, nobody intends to pay: free trial / Always Free
			verdict.Decided, verdict.IsPaid = true, false
		case payModel != "":
			// No promotion at all but a payment model: paid subscription
			verdict.Decided, verdict.IsPaid = true, true
			evidence = append(evidence, "paymentModel="+verdict.PaymentModel)
		}
		_ = promoActive

		if verdict.PaymentModel != "" && !strings.Contains(strings.Join(evidence, ","), "paymentModel=") {
			evidence = append(evidence, "paymentModel="+verdict.PaymentModel)
		}
		if verdict.Decided {
			break
		}
	}

	if !verdict.Found {
		verdict.Reason = "Organizations API 未返回任何订阅"
		return verdict, nil
	}
	label := "未能判定"
	if verdict.Decided {
		if verdict.IsPaid {
			label = "已升级 (PAYG)"
		} else {
			label = "免费号 (Free Tier)"
		}
	}
	verdict.Reason = fmt.Sprintf("Organizations 订阅接口: %s [%s]", label, strings.Join(evidence, "; "))
	return verdict, nil
}

// AccountIdentity is the human-facing identity of the account behind a profile.
type AccountIdentity struct {
	Email       string
	UserCreated *time.Time
	TenancyName string
}

// GetAccountIdentity reads the API user's email and creation time plus the tenancy name.
func GetAccountIdentity(ctx context.Context, profile *storage.OCIProfile) (*AccountIdentity, error) {
	idClient, err := GetIdentityClient(profile)
	if err != nil {
		return nil, err
	}
	out := &AccountIdentity{}

	userResp, err := idClient.GetUser(ctx, identity.GetUserRequest{UserId: common.String(profile.UserOCID)})
	if err != nil {
		return nil, err
	}
	out.Email = StrVal(userResp.User.Email)
	if out.Email == "" && strings.Contains(StrVal(userResp.User.Name), "@") {
		out.Email = StrVal(userResp.User.Name)
	}
	if userResp.User.TimeCreated != nil {
		t := userResp.User.TimeCreated.Time
		out.UserCreated = &t
	}

	if tenancyResp, err := idClient.GetTenancy(ctx, identity.GetTenancyRequest{TenancyId: common.String(profile.TenancyOCID)}); err == nil {
		out.TenancyName = StrVal(tenancyResp.Tenancy.Name)
	}
	return out, nil
}

// EnrichProfileIdentity stores email, registration time, tenancy name, country and the
// subscription verdict on the profile so the account list can show them without live calls.
func EnrichProfileIdentity(ctx context.Context, profile *storage.OCIProfile) {
	updates := map[string]interface{}{}

	if ident, err := GetAccountIdentity(ctx, profile); err == nil {
		if ident.Email != "" {
			updates["account_email"] = ident.Email
		}
		if ident.TenancyName != "" {
			updates["tenancy_name"] = ident.TenancyName
		}
		if ident.UserCreated != nil {
			updates["account_created_at"] = ident.UserCreated
		}
	}

	homeRegion, err := ResolveHomeRegion(ctx, profile)
	if err != nil {
		homeRegion = profile.Region
	}
	if verdict, err := DetectAccountTypeBySubscription(ctx, profile, homeRegion); err == nil && verdict.Found {
		if verdict.CountryCode != "" {
			updates["country_code"] = verdict.CountryCode
		}
		if verdict.StartDate != nil {
			updates["account_created_at"] = verdict.StartDate
		}
		if verdict.Decided {
			if verdict.IsPaid {
				updates["detected_type"] = "PAYG"
			} else {
				updates["detected_type"] = "FREE_TIER"
			}
			updates["detection_reason"] = verdict.Reason
			updates["detection_source"] = "subscription"
		}
	}

	if len(updates) > 0 {
		storage.DB.Model(profile).Updates(updates)
	}
}
