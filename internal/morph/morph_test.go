package morph

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tk, err := New()
	require.NoError(t, err)
	require.NotNil(t, tk)
}

func TestTokenize(t *testing.T) {
	tk, err := New()
	require.NoError(t, err)
	require.NotNil(t, tk)

	tokens := tk.Tokenize("すもももももももものうち")
	require.NotEmpty(t, tokens)

	// Every token must carry a non-empty surface form.
	for _, tok := range tokens {
		require.NotEmpty(t, tok.Surface)
	}

	// At least one token must have a determined part of speech.
	hasPOS := false
	for _, tok := range tokens {
		if tok.POS != "" {
			hasPOS = true
			break
		}
	}
	require.True(t, hasPOS, "expected at least one token with a non-empty POS")
}

func TestTokenizeEmpty(t *testing.T) {
	tk, err := New()
	require.NoError(t, err)

	// 空文字列でもpanicせず空スライス相当を返すことを確認する。
	tokens := tk.Tokenize("")
	require.Empty(t, tokens)
}

func TestTokenizeNilReceiverSafe(t *testing.T) {
	// nilレシーバや未初期化トークナイザでもpanicしないことを保証する。
	var tk *Tokenizer
	require.Empty(t, tk.Tokenize("test"))

	require.Empty(t, (&Tokenizer{}).Tokenize("test"))
}

func TestTokenizeBaseFormFallback(t *testing.T) {
	tk, err := New()
	require.NoError(t, err)

	tokens := tk.Tokenize("食べた")
	require.NotEmpty(t, tokens)

	// 全トークンのBaseFormが非空であること(Surfaceフォールバックにより保証される)。
	for _, tok := range tokens {
		require.NotEmpty(t, tok.BaseForm)
	}
}
