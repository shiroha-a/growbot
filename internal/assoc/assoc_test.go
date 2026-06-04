package assoc

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"growbot/internal/morph"
)

func TestNouns(t *testing.T) {
	tokens := []morph.Token{
		{Surface: "猫", POS: "名詞"},
		{Surface: "が", POS: "助詞"},
		{Surface: "魚", POS: "名詞"},
		{Surface: "を", POS: "助詞"},
		{Surface: "食べる", POS: "動詞"},
		// 重複する名詞は初出のみ残す。
		{Surface: "猫", POS: "名詞"},
		// 空のSurfaceはスキップする。
		{Surface: "", POS: "名詞"},
	}

	got := Nouns(tokens)
	require.Equal(t, []string{"猫", "魚"}, got)
}

func TestNounsNil(t *testing.T) {
	require.Empty(t, Nouns(nil))
	require.Empty(t, Nouns([]morph.Token{}))
}

func TestNounsNoNouns(t *testing.T) {
	tokens := []morph.Token{
		{Surface: "が", POS: "助詞"},
		{Surface: "食べる", POS: "動詞"},
	}
	require.Empty(t, Nouns(tokens))
}

func TestPickSeedDeterministic(t *testing.T) {
	tokens := []morph.Token{
		{Surface: "猫", POS: "名詞"},
		{Surface: "が", POS: "助詞"},
		{Surface: "魚", POS: "名詞"},
		{Surface: "犬", POS: "名詞"},
	}
	nouns := Nouns(tokens)

	// 同一シードからは同一の選択結果が得られることを確認する。
	r1 := rand.New(rand.NewSource(42))
	r2 := rand.New(rand.NewSource(42))

	seed1, ok1 := PickSeed(tokens, r1)
	require.True(t, ok1)
	require.Contains(t, nouns, seed1)

	seed2, ok2 := PickSeed(tokens, r2)
	require.True(t, ok2)
	require.Equal(t, seed1, seed2)
}

func TestPickSeedNilRand(t *testing.T) {
	tokens := []morph.Token{
		{Surface: "猫", POS: "名詞"},
	}
	// rがnilでも時刻シードで動作し、唯一の名詞を返す。
	seed, ok := PickSeed(tokens, nil)
	require.True(t, ok)
	require.Equal(t, "猫", seed)
}

func TestPickSeedNoNouns(t *testing.T) {
	tokens := []morph.Token{
		{Surface: "が", POS: "助詞"},
		{Surface: "食べる", POS: "動詞"},
	}
	r := rand.New(rand.NewSource(1))
	seed, ok := PickSeed(tokens, r)
	require.False(t, ok)
	require.Equal(t, "", seed)
}

func TestPickSeedNilInput(t *testing.T) {
	seed, ok := PickSeed(nil, rand.New(rand.NewSource(1)))
	require.False(t, ok)
	require.Equal(t, "", seed)
}
