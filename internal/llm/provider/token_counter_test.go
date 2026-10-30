package provider

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/raulaguila/opcode/internal/llm/models"
	"github.com/raulaguila/opcode/internal/message"
	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer"
)

func TestRequestTokenCounterMatchesNativeCodec(t *testing.T) {
	for _, model := range []string{"gpt-4o", "gpt-4"} {
		t.Run(model, func(t *testing.T) {
			native, err := tokenizer.ForModel(tokenizer.Model(model))
			require.NoError(t, err)
			counter := newRequestTokenCounter(models.Model{Provider: models.ProviderOpenAI, APIModel: model})
			for _, text := range []string{"", "hello world", "Olá, 日本語 👩🏽‍💻", string([]byte{255, 'a'}), `{"tools":[{"name":"read","description":"Read one file"}]}`} {
				want, err := native.Count(text)
				require.NoError(t, err)
				for range 3 {
					got, err := counter.count([]byte(text))
					require.NoError(t, err)
					require.Equal(t, want, got)
				}
			}
			require.Equal(t, native.GetName(), counter.GetName())
		})
	}
}

type countingCodec struct {
	tokenizer.Codec
	calls    int
	failures int
}

func (c *countingCodec) GetName() string { return "fixture" }
func (c *countingCodec) Count(text string) (int, error) {
	c.calls++
	if c.failures > 0 {
		c.failures--
		return 0, errors.New("temporary count failure")
	}
	return len(text), nil
}

func TestRequestTokenCounterBoundsExactCacheWithoutCachingFailures(t *testing.T) {
	codec := &countingCodec{failures: 1}
	counter := newRequestTokenCounter(models.Model{Provider: models.ProviderOpenAI, APIModel: "fixture"})
	counter.once.Do(func() { counter.codec = codec; counter.name = codec.GetName() })
	_, err := counter.count([]byte("alpha"))
	require.Error(t, err)
	for range 2 {
		count, err := counter.count([]byte("alpha"))
		require.NoError(t, err)
		require.Equal(t, 5, count)
	}
	require.Equal(t, 2, codec.calls, "a failed count must be retried; an exact successful count is reused")
	for _, text := range []string{"bravo", "delta", "alpha"} {
		_, err = counter.count([]byte(text))
		require.NoError(t, err)
	}
	require.Equal(t, 5, codec.calls, "equal-length changed payloads invalidate; only two entries survive")
}

func tokenizerFixture(t testing.TB, model string) *baseProvider {
	t.Helper()
	p, err := NewProvider(models.ProviderOpenAI, WithAPIKey("fixture"), WithModel(models.Model{Provider: models.ProviderOpenAI, APIModel: model, ContextWindow: 128000}), WithMaxTokens(2048))
	require.NoError(t, err)
	return p.(*baseProvider)
}

func TestRequestTokenCounterOwnershipAndConcurrentClones(t *testing.T) {
	original := tokenizerFixture(t, "gpt-4o")
	clone, err := original.WithSystemPrompt("scoped prompt")
	require.NoError(t, err)
	clone, err = clone.(ContextWindowController).WithContextWindow(32768)
	require.NoError(t, err)
	clone, err = clone.(SourceCitationController).WithSourceCitations([]string{"main.go:1"})
	require.NoError(t, err)
	require.Same(t, original.tokenCounter, clone.(*baseProvider).tokenCounter)
	require.NotSame(t, original.tokenCounter, tokenizerFixture(t, "gpt-4o").tokenCounter, "connections cannot share prompt caches")
	other := tokenizerFixture(t, "gpt-4")
	require.NotEqual(t, original.tokenCounter.GetName(), other.tokenCounter.GetName())
	require.False(t, original.tokenCounter.matches(other.Model()))
	local := original.Model()
	local.Provider = models.ProviderLocal
	require.False(t, original.tokenCounter.matches(local))
	verifyConcurrentTokenCounts(t, original, clone.(*baseProvider))
	require.Equal(t, "", original.options.systemMessage)
	require.Empty(t, original.options.sourceCitations)
}

func verifyConcurrentTokenCounts(t *testing.T, providers ...*baseProvider) {
	t.Helper()
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			p := providers[worker%len(providers)]
			native, err := tokenizer.ForModel(tokenizer.Model(p.Model().APIModel))
			require.NoError(t, err)
			for i := range 6 {
				history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("Olá worker %d turn %d", worker, i)}}}}
				request, err := p.PrepareRequest(history, nil, true)
				require.NoError(t, err)
				prepared := request.(*preparedHTTP)
				expected, err := native.Count(string(prepared.body))
				require.NoError(t, err)
				footprint, err := request.RequestFootprint()
				require.NoError(t, err)
				require.EqualValues(t, expected, footprint.TokenizedPayloadTokens)
			}
		})
	}
	wg.Wait()
}
