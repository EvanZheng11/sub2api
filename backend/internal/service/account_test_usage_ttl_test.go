//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountTestUsageOpenAICacheWriteTTL(t *testing.T) {
	for _, tc := range []struct {
		name        string
		short, long int
	}{
		{"30m", 100, 0}, {"12h", 0, 100}, {"mixed", 30, 70},
	} {
		for _, protocol := range []string{"responses", "chat"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%v", tc.name, protocol, stream), func(t *testing.T) {
					input, output, details := "input_tokens", "output_tokens", "input_tokens_details"
					if protocol == "chat" {
						input, output, details = "prompt_tokens", "completion_tokens", "prompt_tokens_details"
					}
					payload := fmt.Sprintf(`{"usage":{"%s":123,"%s":5,"%s":{"cached_tokens":20,"cache_write_tokens":100,"cache_write_tokens_details":{"ttl_30m_tokens":%d,"ttl_12h_tokens":%d}}}}`, input, output, details, tc.short, tc.long)
					body := payload
					if stream {
						body = "data: " + payload + "\n\ndata: " + payload + "\n\n"
					}
					r := &accountTestUsageRequest{model: "gpt-6-luna"}
					capture := &accountTestUsageBody{ReadCloser: io.NopCloser(strings.NewReader(body)), request: r}
					_, err := io.ReadAll(capture)
					require.NoError(t, err)
					require.NoError(t, capture.Close())
					require.Equal(t, UsageTokens{InputTokens: 3, OutputTokens: 5, CacheReadTokens: 20, CacheCreationTokens: 100, CacheCreation30mTokens: tc.short, CacheCreation12hTokens: tc.long}, r.tokens)
					ledger := &accountTestBillingStub{pricing: NewBillingService(nil, nil)}
					session := &accountTestUsageSession{admin: &User{ID: 900}, key: &APIKey{ID: 901}, account: &Account{ID: 55}, billing: ledger, requests: []*accountTestUsageRequest{r}}
					require.NoError(t, session.finish(context.Background()))
					require.Len(t, ledger.logs, 1)
					log := ledger.logs[0]
					require.Equal(t, tc.short, log.CacheCreation30mTokens)
					require.Equal(t, tc.long, log.CacheCreation12hTokens)
					cost, err := ledger.calculate(context.Background(), r)
					require.NoError(t, err)
					require.InDelta(t, (float64(tc.short)*0.125+float64(tc.long)*0.25)/1e6, cost.CacheCreationCost, 1e-12)
					require.Equal(t, QuantizeUsageBillingAmount(cost.ActualCost), log.ActualCost)
				})
			}
		}
	}
}

func TestAccountTestUsageOpenAICacheWriteTTLInvalidDetails(t *testing.T) {
	body := `{"usage":{"input_tokens":100,"input_tokens_details":{"cache_write_tokens":100,"cache_write_tokens_details":{"ttl_30m_tokens":30,"ttl_12h_tokens":20}}}}`
	r := &accountTestUsageRequest{}
	b := &accountTestUsageBody{ReadCloser: io.NopCloser(strings.NewReader(body)), request: r}
	_, err := io.ReadAll(b)
	require.NoError(t, err)
	require.NoError(t, b.Close())
	require.Zero(t, r.tokens.InputTokens)
	require.Equal(t, 100, r.tokens.CacheCreationTokens)
	require.Zero(t, r.tokens.CacheCreation30mTokens)
	require.Zero(t, r.tokens.CacheCreation12hTokens)
}
