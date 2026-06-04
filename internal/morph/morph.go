// Package morph provides a thin wrapper around the kagome v2 morphological
// analyzer using the IPA dictionary. It exposes a minimal, stable Token type
// so that the rest of the codebase does not depend on kagome internals.
package morph

import (
	"fmt"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome/v2/tokenizer"
)

// ipaBaseFormIndex is the feature index of the base form in the IPA dictionary
// contents. IPA辞書のfeature配列では先頭4要素が品詞階層、続いて活用型・活用形、
// インデックス6が原形(基本形)となる。BaseForm()が利用できない場合の保険として用いる。
const ipaBaseFormIndex = 6

// Token represents a single morpheme produced by the analyzer.
type Token struct {
	// Surface is the literal text of the morpheme as it appears in the input.
	Surface string
	// POS is the major (first) part-of-speech element. It may be empty when the
	// analyzer cannot determine a part of speech.
	POS string
	// BaseForm is the dictionary (lemma) form of the morpheme. It falls back to
	// Surface when the base form is unavailable.
	BaseForm string
}

// Tokenizer wraps a kagome v2 tokenizer configured with the IPA dictionary.
type Tokenizer struct {
	t *tokenizer.Tokenizer
}

// New creates a Tokenizer backed by the embedded IPA dictionary.
// BOS/EOS markers are omitted from the output.
func New() (*Tokenizer, error) {
	t, err := tokenizer.New(ipa.Dict(), tokenizer.OmitBosEos())
	if err != nil {
		return nil, fmt.Errorf("morph: new tokenizer: %w", err)
	}
	return &Tokenizer{t: t}, nil
}

// Tokenize splits text into morphemes. It never panics: all slice accesses are
// bounds-checked, and BOS/EOS dummy tokens are skipped defensively.
func (t *Tokenizer) Tokenize(text string) []Token {
	if t == nil || t.t == nil {
		return nil
	}
	raw := t.t.Tokenize(text)
	out := make([]Token, 0, len(raw))
	for _, tok := range raw {
		// OmitBosEos()を指定していてもBOS/EOS相当のダミートークンが混入する可能性に備える。
		if tok.Class == tokenizer.DUMMY {
			continue
		}

		tk := Token{
			Surface:  tok.Surface,
			BaseForm: tok.Surface,
		}

		// 主要品詞(品詞大分類)はPOS()の先頭要素。空ならFeatures()の先頭で補う。
		if pos := tok.POS(); len(pos) > 0 {
			tk.POS = pos[0]
		} else if feats := tok.Features(); len(feats) > 0 {
			tk.POS = feats[0]
		}

		// 原形はBaseForm()を優先し、取得できなければFeaturesのインデックス6を厳密境界チェックで参照する。
		if base, ok := tok.BaseForm(); ok && base != "" && base != "*" {
			tk.BaseForm = base
		} else if feats := tok.Features(); len(feats) > ipaBaseFormIndex {
			if v := feats[ipaBaseFormIndex]; v != "" && v != "*" {
				tk.BaseForm = v
			}
		}

		out = append(out, tk)
	}
	return out
}
