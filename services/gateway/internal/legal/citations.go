package legal

import (
	"regexp"
	"strings"
)

// These patterns MUST stay in sync with services/worker/app/etl.py
// (_ARTICLE_RE / _LAW_RE / _DECREE_RE / _CNPJ_RE). If the worker extracts
// "8.666/1993" as a law, the verifier below must recognize the same string in
// the LLM's answer. Drift silently breaks citation verification.
var (
	articleRE = regexp.MustCompile(`(?i)\bArt(?:igo)?\.?\s*(\d+[º°ªo]?(?:[.\-]\d+)*)`)
	lawRE     = regexp.MustCompile(`(?i)\bLei(?:\s+Complementar|\s+Ordinária)?\s+n?[º°o.]*\s*([\d.\-/]+)`)
	decreeRE  = regexp.MustCompile(`(?i)\bDecreto(?:\s+Federal)?\s+n?[º°o.]*\s*([\d.\-/]+)`)
	cnpjRE    = regexp.MustCompile(`\b\d{2}\.\d{3}\.\d{3}/\d{4}-\d{2}\b`)
)

// Citations groups the references pulled out of a text. Keys mirror the
// entity categories written by the worker: "articles", "laws", "decrees",
// "cnpjs". Empty categories are omitted.
type Citations map[string][]string

// Flatten returns all citations as a single list for quick iteration.
func (c Citations) Flatten() []Citation {
	out := make([]Citation, 0)
	for kind, values := range c {
		for _, v := range values {
			out = append(out, Citation{Kind: kind, Value: v})
		}
	}
	return out
}

// Citation is one reference like Art. 150 or Lei 8.666/1993.
type Citation struct {
	Kind  string `json:"kind"`  // "articles" | "laws" | "decrees" | "cnpjs"
	Value string `json:"value"` // e.g. "150" or "8.666/1993"
}

// ExtractCitations returns the legal references present in text, deduped per
// category. The output key set matches the Qdrant payload keys so
// verification can be a simple map lookup.
func ExtractCitations(text string) Citations {
	out := Citations{}
	if arts := collect(articleRE, text); len(arts) > 0 {
		out["articles"] = arts
	}
	if laws := collect(lawRE, text); len(laws) > 0 {
		out["laws"] = laws
	}
	if decrees := collect(decreeRE, text); len(decrees) > 0 {
		out["decrees"] = decrees
	}
	if cnpjs := collectFull(cnpjRE, text); len(cnpjs) > 0 {
		out["cnpjs"] = cnpjs
	}
	return out
}

// VerifyCitations returns the subset of `cited` that cannot be found in any
// of `chunkEntities`. An empty return value means every citation in the
// answer is grounded in a retrieved chunk.
//
// Matching is done per-kind (an "article" cited in the answer is only
// considered verified if it appears under "articles" of some chunk — a
// number coincidentally matching under "decrees" does not count).
//
// Values are normalized with normalize() before comparison to paper over
// trivial differences like trailing periods or non-breaking spaces.
func VerifyCitations(cited Citations, chunkEntities []map[string][]string) []Citation {
	known := make(map[string]map[string]struct{}, 4)
	for _, ent := range chunkEntities {
		for kind, values := range ent {
			set, ok := known[kind]
			if !ok {
				set = make(map[string]struct{})
				known[kind] = set
			}
			for _, v := range values {
				set[normalize(v)] = struct{}{}
			}
		}
	}

	var unverified []Citation
	for _, c := range cited.Flatten() {
		set, ok := known[c.Kind]
		if !ok {
			unverified = append(unverified, c)
			continue
		}
		if _, found := set[normalize(c.Value)]; !found {
			unverified = append(unverified, c)
		}
	}
	return unverified
}

func collect(re *regexp.Regexp, text string) []string {
	matches := re.FindAllStringSubmatch(text, -1)
	return dedup(matches, 1)
}

func collectFull(re *regexp.Regexp, text string) []string {
	matches := re.FindAllStringSubmatch(text, -1)
	return dedup(matches, 0)
}

func dedup(matches [][]string, captureIdx int) []string {
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) <= captureIdx {
			continue
		}
		v := strings.TrimRight(strings.TrimSpace(m[captureIdx]), "./-")
		if v == "" {
			continue
		}
		key := normalize(v)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, v)
	}
	return out
}

// normalize makes two citation strings comparable: lowercases, strips spaces,
// collapses unicode ordinal markers (º/°/ª) to ASCII. Keeps punctuation like
// "/" and "." because those are part of the law identifier ("8.666/1993").
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "º", "")
	s = strings.ReplaceAll(s, "°", "")
	s = strings.ReplaceAll(s, "ª", "")
	s = strings.TrimSuffix(s, ".")
	return s
}
