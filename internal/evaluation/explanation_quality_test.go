package evaluation

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExplanationQualityFixturesPreserveEquivalentCriteriaAndCurrentSources(t *testing.T) {
	suite, raw, err := Load("testdata/explanation_quality.json")
	require.NoError(t, err)
	require.Len(t, suite.Cases, 4)
	require.Equal(t, suite.Cases[0].Files, suite.Cases[1].Files)
	require.Equal(t, suite.Cases[0].Criteria, suite.Cases[1].Criteria, "PT/EN wording must not change the quality gate")
	require.NotEqual(t, suite.Cases[0].Prompts, suite.Cases[1].Prompts)
	for _, c := range suite.Cases {
		require.Equal(t, "ask", c.Mode, "investigation fixtures never authorize edits or commands")
		for _, criterion := range c.Criteria {
			if criterion.Kind == "response_cites" {
				require.Contains(t, []string{"service.py", "tools_runtime.py"}, criterion.Path)
			}
		}
		base, root, err := Prepare(c)
		require.NoError(t, err)
		defer os.RemoveAll(base)
		wrong := Evaluate(c, root, Observation{Response: "Submit calls dispatchTool: TOOL_DENIED or TOOL_OK. (api_contract.py:3-4)"})
		require.Equal(t, "failed", wrong.Status, "facts without current implementation citations must fail")
		for _, criterion := range wrong.Criteria {
			if criterion.Kind == "response_cites" {
				require.False(t, *criterion.Passed)
			}
		}
		correct := Evaluate(c, root, Observation{Response: "Submit calls dispatchTool (service.py:4). blocked returns TOOL_DENIED; ping returns TOOL_OK (tools_runtime.py:4-6)."})
		require.Equal(t, "passed", correct.Status)
	}
	report, err := Run(context.Background(), suite, raw, Options{})
	require.NoError(t, err)
	require.Equal(t, "offline_fixture_validation", report.Mode)
	for _, c := range report.Cases {
		require.Equal(t, "validated", c.Status)
		for _, criterion := range c.Criteria {
			require.Nil(t, criterion.Passed, "offline validation is not a model-quality result")
		}
	}
}

func TestExplanationQualityRequiresExplicitCorrectReturnRelations(t *testing.T) {
	suite, _, err := Load("testdata/explanation_quality.json")
	require.NoError(t, err)
	c := suite.Cases[0]
	base, root, err := Prepare(c)
	require.NoError(t, err)
	defer os.RemoveAll(base)
	const sources = "Submit calls dispatchTool (service.py:4). tools_runtime.py:4-6.\n"
	for _, answer := range []string{
		"Para blocked, retorna TOOL_DENIED; para ping, retorna TOOL_OK.",
		"blocked: TOOL_DENIED, ping: TOOL_OK.",
		"blocked → TOOL_DENIED | ping → TOOL_OK",
		"For blocked, the return value is TOOL_DENIED. For ping, the return value is TOOL_OK.",
		"For blocked, dispatchTool returns TOOL_DENIED; for ping, the function returns TOOL_OK.",
		"For blocked, it returns TOOL_DENIED; for ping, it returns TOOL_OK.",
		"Para blocked, a função retorna TOOL_DENIED; para ping, ele retorna TOOL_OK.",
		"Para blocked: O retorno é TOOL_DENIED; para ping: O retorno é TOOL_OK.",
		"blocked é TOOL_DENIED; ping é TOOL_OK.",
		"blocked retornará TOOL_DENIED; ping retornará TOOL_OK.",
		"- `blocked` → `TOOL_DENIED`\n- `ping` → `TOOL_OK`",
		"| Input | Return |\n| blocked | TOOL_DENIED |\n| ping | TOOL_OK |",
		"if text == \"blocked\":\n    return \"TOOL_DENIED\"\nif text == \"ping\":\n    return \"TOOL_OK\"",
		"TOOL_DENIED para blocked; TOOL_OK para ping.",
		"blocked → TOOL_DENIED; ping → TOOL_OK. If the input is not blocked, the function returns TOOL_OK.",
		"blocked → TOOL_DENIED; ping → TOOL_OK. Se a entrada não é blocked, a função retorna TOOL_OK.",
		"1. **For \"ping\":** Since the input is not blocked, the observed return value is TOOL_OK (tools_runtime.py:6).\n2. **For \"blocked\" (Failure Path):** The condition holds and execution returns TOOL_DENIED (tools_runtime.py:5).",
		"- **Para \"blocked\":** O comportamento observado é que dispatchTool retorna TOOL_DENIED (tools_runtime.py:5).\n- **Para \"ping\":** O comportamento observado é que dispatchTool retorna TOOL_OK (tools_runtime.py:6).",
		"**Comportamento de `blocked`:** A implementação consulta a entrada e produz TOOL_DENIED.\n**Comportamento de `ping`:** A entrada não coincide com blocked, por isso resulta em TOOL_OK.",
		"Behavior of blocked: The input branch reaches TOOL_DENIED.\nBehavior of ping: The normal branch reaches TOOL_OK.",
	} {
		t.Run(answer, func(t *testing.T) {
			require.Equal(t, "passed", Evaluate(c, root, Observation{Response: sources + answer}).Status)
		})
	}
	for _, answer := range []string{
		"For blocked it returns TOOL_OK; for ping it returns TOOL_DENIED.",
		"Para blocked, retorna TOOL_OK e para ping, retorna TOOL_DENIED.",
		"blocked é TOOL_OK; ping é TOOL_DENIED.",
		"blocked retornará TOOL_OK; ping retornará TOOL_DENIED.",
		"For blocked, dispatchTool returns TOOL_OK; for ping, the function returns TOOL_DENIED.",
		"| Input | Return |\n| blocked | TOOL_OK |\n| ping | TOOL_DENIED |",
		"blocked returns TOOL_DENIED. TOOL_OK also exists.",
		"blocked and ping produce TOOL_DENIED or TOOL_OK.",
		"unblocked returns TOOL_DENIED; pinging returns TOOL_OK.",
		"blocked returns TOOL_DENIED; ping returns TOOL_OK. blocked returns TOOL_OK.",
		"blocked é TOOL_DENIED; ping é TOOL_OK. blocked é TOOL_OK.",
		"blocked → TOOL_DENIED; ping → TOOL_OK. For blocked, the function returns TOOL_OK.",
		"blocked → TOOL_DENIED; ping → TOOL_OK. Para blocked: O retorno é TOOL_OK.",
		"blocked → TOOL_DENIED; ping → TOOL_OK.\n- blocked → TOOL_OK",
		"blocked → TOOL_DENIED; ping → TOOL_OK.\n| blocked | TOOL_OK |",
		"blocked → TOOL_DENIED; ping → TOOL_OK.\nif text == \"blocked\":\n    return \"TOOL_OK\"",
		"1. **For \"ping\":** Since the input is not blocked, the observed return value is TOOL_DENIED.\n2. **For \"blocked\":** The condition holds and execution returns TOOL_OK.",
		"- **Para \"blocked\":** O comportamento observado é que dispatchTool retorna TOOL_OK.\n- **Para \"ping\":** O comportamento observado é que dispatchTool retorna TOOL_DENIED.",
		"Comportamento de blocked: A implementação produz TOOL_OK.\nComportamento de ping: A implementação produz TOOL_DENIED.",
		"Behavior of blocked: It reaches TOOL_OK.\nBehavior of ping: It reaches TOOL_DENIED.",
		"blocked → TOOL_DENIED; ping → TOOL_OK.\nFor blocked: After inspecting the branch, the result is TOOL_OK.",
		"For blocked: No return stated.\nAn unrelated paragraph mentions TOOL_DENIED.\nFor ping: No return stated.\nAn unrelated paragraph mentions TOOL_OK.",
		"Project overview: blocked and ping mention TOOL_DENIED and TOOL_OK without relating them.",
	} {
		t.Run(answer, func(t *testing.T) {
			require.Equal(t, "failed", Evaluate(c, root, Observation{Response: sources + answer}).Status)
		})
	}
}
