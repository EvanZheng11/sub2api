//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 入口回归使用生产 calculate，不用返回固定价格的 stub 隐藏缺价问题。
type prefixedTestLedger struct {
	*accountTestBilling
	logs []*UsageLog
}

func (b *prefixedTestLedger) prepare(context.Context) (*User, *APIKey, error) {
	return &User{ID: 900, Role: RoleAdmin, Status: StatusActive}, &APIKey{ID: 901, UserID: 900, Status: StatusAPIKeyDisabled}, nil
}
func (b *prefixedTestLedger) record(_ context.Context, log *UsageLog) error {
	b.logs = append(b.logs, log)
	return nil
}

func TestAccountTestUsagePrefixedPricing(t *testing.T) {
	billing := &accountTestBilling{pricing: NewBillingService(nil, nil)}
	for _, model := range []string{"deepseek-v4.1-flash", "deepseek-v4-pro"} {
		t.Run(model, func(t *testing.T) {
			tokens := UsageTokens{InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 500}
			want, err := billing.calculate(context.Background(), &accountTestUsageRequest{model: model, tokens: tokens})
			require.NoError(t, err)
			request := &accountTestUsageRequest{model: "workbuddy/" + model, tokens: tokens}
			got, err := billing.calculate(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Positive(t, got.ActualCost)
			require.Equal(t, "workbuddy/"+model, request.model)
		})
	}
}

func TestAccountTestUsagePrefixedChatEntry(t *testing.T) {
	const model = "workbuddy/deepseek-v4.1-flash"
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Name = "my-magpie-workbuddy"
	account.Credentials["api_protocol"] = APIProtocolChatCompletions
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"prompt_tokens_details\":{\"cached_tokens\":500}}}\n\ndata: [DONE]\n\n"))}
	svc, upstream := adaptiveCNAccountTestService(account, response)
	ledger := &prefixedTestLedger{accountTestBilling: &accountTestBilling{pricing: NewBillingService(nil, nil)}}
	svc.testBilling = ledger
	c, w := newTestContext()
	require.NoError(t, svc.TestAccountConnection(c, 1, model, "hi", AccountTestModeDefault))
	require.Len(t, upstream.requests, 1)
	require.Equal(t, model, gjson.GetBytes(upstream.bodies[0], "model").String())
	require.Contains(t, w.Body.String(), `"success":true`)
	require.Len(t, ledger.logs, 1)
	log := ledger.logs[0]
	require.Equal(t, model, log.Model)
	require.Equal(t, int64(900), log.UserID)
	require.Equal(t, int64(901), log.APIKeyID)
	require.Equal(t, 500, log.InputTokens)
	require.Equal(t, 500, log.CacheReadTokens)
	require.Equal(t, 100, log.OutputTokens)
	require.Positive(t, log.ActualCost)
}

func TestAccountTestUsagePrefixedUnknownStillRejected(t *testing.T) {
	billing := &accountTestBilling{pricing: NewBillingService(nil, nil)}
	_, err := billing.calculate(context.Background(), &accountTestUsageRequest{model: "workbuddy/completely-unpriced-model"})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Credentials["api_protocol"] = APIProtocolChatCompletions
	svc, upstream := adaptiveCNAccountTestService(account)
	ledger := &prefixedTestLedger{accountTestBilling: billing}
	svc.testBilling = ledger
	c, _ := newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, 1, "workbuddy/completely-unpriced-model", "hi", AccountTestModeDefault), "已停止调用上游")
	require.Empty(t, upstream.requests)
	require.Empty(t, ledger.logs)
}

func TestAccountTestUsagePrefixedExplicitPrice(t *testing.T) {
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"workbuddy/custom-ledger-model": {InputCostPerToken: 0.001, OutputCostPerToken: 0.002},
		"custom-ledger-model":           {InputCostPerToken: 0.003, OutputCostPerToken: 0.004},
	}}
	billing := &accountTestBilling{pricing: NewBillingService(nil, pricing)}
	request := &accountTestUsageRequest{model: "workbuddy/custom-ledger-model", tokens: UsageTokens{InputTokens: 10, OutputTokens: 5}}
	want, err := billing.pricing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: request.model, Tokens: request.tokens, RateMultiplier: 1})
	require.NoError(t, err)
	got, err := billing.calculate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.InDelta(t, 0.02, got.ActualCost, 1e-12)
}
