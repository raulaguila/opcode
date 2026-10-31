package agent

// SourceCitation is a reference written in a response, not proof of a source read.
type SourceCitation struct {
	Path               string
	StartLine, EndLine int
}

// SourceCitations shares native citation recognition with consumers such as the
// source browser. Provenance, line coverage and freshness still need validation.
func SourceCitations(answer string) []SourceCitation {
	refs := sourceReferences(answer)
	out := make([]SourceCitation, len(refs))
	for i, ref := range refs {
		out[i] = SourceCitation{Path: ref.path, StartLine: ref.first, EndLine: ref.last}
	}
	return out
}
