package habit

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPickCatchphrase_SkipsEmptyDeterministic(t *testing.T) {
	candidates := []string{"ね", "よ", ""}
	// 同じシードからは常に同じ結果が得られることを確認する。
	for seed := int64(0); seed < 100; seed++ {
		r := rand.New(rand.NewSource(seed))
		got, ok := PickCatchphrase(candidates, r)
		require.True(t, ok)
		require.Contains(t, []string{"ね", "よ"}, got)
		require.NotEqual(t, "", got)

		again := rand.New(rand.NewSource(seed))
		got2, ok2 := PickCatchphrase(candidates, again)
		require.Equal(t, ok, ok2)
		require.Equal(t, got, got2)
	}
}

func TestPickCatchphrase_EmptyOrNil(t *testing.T) {
	r := rand.New(rand.NewSource(1))

	got, ok := PickCatchphrase(nil, r)
	require.False(t, ok)
	require.Equal(t, "", got)

	got, ok = PickCatchphrase([]string{}, r)
	require.False(t, ok)
	require.Equal(t, "", got)

	got, ok = PickCatchphrase([]string{"", ""}, r)
	require.False(t, ok)
	require.Equal(t, "", got)
}

func TestPickCatchphrase_NilRand(t *testing.T) {
	got, ok := PickCatchphrase([]string{"ね"}, nil)
	require.True(t, ok)
	require.Equal(t, "ね", got)
}

func TestApply_P1AppendsOnceNoDouble(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	first := Apply("こんにちは", "ね", 1, r)
	require.Equal(t, "こんにちはね", first)

	// HasSuffixガードにより二重付与されないことを確認する。
	second := Apply(first, "ね", 1, r)
	require.Equal(t, first, second)
}

func TestApply_P0NeverAppends(t *testing.T) {
	for seed := int64(0); seed < 50; seed++ {
		r := rand.New(rand.NewSource(seed))
		require.Equal(t, "やあ", Apply("やあ", "ね", 0, r))
	}
}

func TestApply_EmptyInputsUnchanged(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	require.Equal(t, "", Apply("", "ね", 1, r))
	require.Equal(t, "テキスト", Apply("テキスト", "", 1, r))
}

func TestApply_ClampAndDeterministic(t *testing.T) {
	// p>1は1に丸められ、必ず付与される。
	require.Equal(t, "おはようね", Apply("おはよう", "ね", 1.5, rand.New(rand.NewSource(0))))

	// 同じシードからは同じ判定になる。
	a := Apply("test", "!", 0.5, rand.New(rand.NewSource(42)))
	b := Apply("test", "!", 0.5, rand.New(rand.NewSource(42)))
	require.Equal(t, a, b)
}

func TestApply_OutputHasInputPrefixNoControlChars(t *testing.T) {
	inputs := []string{"hello", "こんにちは", "a b c"}
	phrases := []string{"ね", "よ", "!"}
	for _, in := range inputs {
		for _, ph := range phrases {
			for seed := int64(0); seed < 20; seed++ {
				out := Apply(in, ph, 0.5, rand.New(rand.NewSource(seed)))
				require.True(t, strings.HasPrefix(out, in),
					"output %q must have input %q as prefix", out, in)
				for _, ru := range out {
					require.False(t, ru < 0x20 || ru == 0x7f,
						"output %q must not contain control characters", out)
				}
			}
		}
	}
}
