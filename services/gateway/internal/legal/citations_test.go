package legal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractCitations_Articles(t *testing.T) {
	text := "Conforme o Art. 150 da Constituição e o artigo 7º do Decreto..."
	c := ExtractCitations(text)
	assert.ElementsMatch(t, []string{"150", "7º"}, c["articles"])
}

func TestExtractCitations_Laws(t *testing.T) {
	text := "Aplica-se a Lei 8.666/1993 e a Lei Complementar nº 123/2006."
	c := ExtractCitations(text)
	assert.ElementsMatch(t, []string{"8.666/1993", "123/2006"}, c["laws"])
}

func TestExtractCitations_Decrees(t *testing.T) {
	text := "O Decreto 10.024/2019 e o Decreto Federal nº 9.203/2017 regulam o tema."
	c := ExtractCitations(text)
	assert.ElementsMatch(t, []string{"10.024/2019", "9.203/2017"}, c["decrees"])
}

func TestExtractCitations_CNPJ(t *testing.T) {
	text := "A empresa 12.345.678/0001-99 foi autuada."
	c := ExtractCitations(text)
	assert.Equal(t, []string{"12.345.678/0001-99"}, c["cnpjs"])
}

func TestExtractCitations_PlainText_Empty(t *testing.T) {
	c := ExtractCitations("Bom dia, qual o horário da mudança?")
	assert.Empty(t, c)
}

func TestExtractCitations_Dedup(t *testing.T) {
	text := "Art. 150 é o mesmo que Artigo 150. Lei 8.666/1993, Lei nº 8.666/1993."
	c := ExtractCitations(text)
	assert.Equal(t, 1, len(c["articles"]))
	assert.Equal(t, 1, len(c["laws"]))
}

func TestVerifyCitations_AllGrounded(t *testing.T) {
	cited := Citations{
		"articles": []string{"150"},
		"laws":     []string{"8.666/1993"},
	}
	chunks := []map[string][]string{
		{"articles": []string{"150", "7º"}, "laws": []string{"8.666/1993"}},
	}
	unverified := VerifyCitations(cited, chunks)
	assert.Empty(t, unverified)
}

func TestVerifyCitations_HallucinatedLaw(t *testing.T) {
	cited := Citations{
		"laws": []string{"8.666/1993", "9999/2099"}, // second is fake
	}
	chunks := []map[string][]string{
		{"laws": []string{"8.666/1993"}},
	}
	unverified := VerifyCitations(cited, chunks)
	assert.Equal(t, []Citation{{Kind: "laws", Value: "9999/2099"}}, unverified)
}

func TestVerifyCitations_WrongCategoryNotCredited(t *testing.T) {
	// Chunk has "150" as a decree; answer cites "Art. 150". Must fail.
	cited := Citations{"articles": []string{"150"}}
	chunks := []map[string][]string{
		{"decrees": []string{"150"}},
	}
	unverified := VerifyCitations(cited, chunks)
	assert.Len(t, unverified, 1)
}

func TestVerifyCitations_EmptyChunkEntities(t *testing.T) {
	cited := Citations{"articles": []string{"1"}}
	unverified := VerifyCitations(cited, nil)
	assert.Len(t, unverified, 1)
}

func TestVerifyCitations_OrdinalMarkerIgnored(t *testing.T) {
	// Answer says "Art. 7º", chunk has "7".
	cited := Citations{"articles": []string{"7º"}}
	chunks := []map[string][]string{
		{"articles": []string{"7"}},
	}
	unverified := VerifyCitations(cited, chunks)
	assert.Empty(t, unverified)
}

func TestExtractCitations_ArticlesWithNumericSubitems(t *testing.T) {
	// The regex (shared with etl.py) captures digit runs separated by dots
	// or dashes, so "7.1" is captured in full but "150-A" stops at "150"
	// (letters after dash aren't part of the spec). Both verticals agree
	// on this behavior.
	text := "O Art. 150-A e o Art. 7.1 do CTN."
	c := ExtractCitations(text)
	assert.ElementsMatch(t, []string{"150", "7.1"}, c["articles"])
}
