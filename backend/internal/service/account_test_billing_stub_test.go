package service

import "context"

// 旧协议测试显式使用 mock 账本，不让生产路径静默跳过计费。
type accountTestBillingStub struct {
	logs                  []*UsageLog
	prepareErr, recordErr error
	pricing               *BillingService
}

func (b *accountTestBillingStub) prepare(context.Context) (*User, *APIKey, error) {
	return &User{ID: 900, Role: RoleAdmin, Status: StatusActive}, &APIKey{ID: 901, UserID: 900, Status: StatusAPIKeyDisabled}, b.prepareErr
}
func (b *accountTestBillingStub) calculate(ctx context.Context, r *accountTestUsageRequest) (*CostBreakdown, error) {
	if b.pricing != nil {
		return b.pricing.CalculateCostUnified(CostInput{Ctx: ctx, Model: r.model, Tokens: r.tokens, RateMultiplier: 1})
	}
	return &CostBreakdown{}, nil
}
func (b *accountTestBillingStub) record(_ context.Context, log *UsageLog) error {
	if b.recordErr != nil {
		return b.recordErr
	}
	b.logs = append(b.logs, log)
	return nil
}
