package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 题库提示词上限为 128000 个字符（rune），标题仍为 200。
func TestNormalizeTestQuestionInputPromptLimitIs128000(t *testing.T) {
	atLimit := strings.Repeat("题", maxTestQuestionPromptLen)
	_, err := normalizeTestQuestionInput(TestQuestionInput{
		Title:  "长提示词",
		Prompt: atLimit,
		Type:   TestQuestionTypeText,
	})
	require.NoError(t, err)

	overLimit := strings.Repeat("题", maxTestQuestionPromptLen+1)
	_, err = normalizeTestQuestionInput(TestQuestionInput{
		Title:  "超长提示词",
		Prompt: overLimit,
		Type:   TestQuestionTypeText,
	})
	require.ErrorIs(t, err, ErrTestQuestionPromptTooLong)

	require.Equal(t, 128000, maxTestQuestionPromptLen)
	require.Equal(t, 200, maxTestQuestionTitleLen)
}
