package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOverviewIndexReturnsImplementationInsteadOfQueryLiteralNoise(t *testing.T) {
	root := productivityV2Workspace(t)
	retrievalFixture(t, root, "service.go", "package service\nfunc collectSamples() string { return \"SENSOR_IMPLEMENTATION\" }\n")
	retrievalFixture(t, root, "bootstrap.go", "package service\nfunc bootstrap() string { return \"service server application DECOY_QUERY_LITERAL\" }\n")
	tool := SecureTool(NewCodeIndexTool(), nil)
	page := searchPage(t, tool, map[string]any{"query": "service server application", "intent": "overview"})
	require.NotEmpty(t, page.Results)
	for _, hit := range page.Results {
		require.Equal(t, "service.go", hit.Path)
		require.NotContains(t, hit.Preview, "DECOY_QUERY_LITERAL")
	}
}

func TestOverviewRankingRetainsBodyMatchesWhenIdentifiersProvideNoLead(t *testing.T) {
	hits := []codeIndexHit{{Path: "sampler.go", Symbol: "Collect", Preview: "MQTT receives sensor samples"}}
	require.Equal(t, hits, overviewIndexCandidates(hits, "MQTT"))
}
