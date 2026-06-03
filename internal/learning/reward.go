// Package learning implements the engagement-learning subsystem for growbot.
//
// It turns the reactions a post received into a scalar reward, drives a
// Thompson-sampling multi-armed bandit over named generation strategies
// ("arms"), and keeps a ledger of posts whose engagement is still pending
// observation. Together these let the bot adapt what it posts based purely on
// how its audience responds, with no LLM and no external services.
package learning

// rewardSaturation controls how quickly the reward approaches its upper bound
// of 1. Larger values require more raw engagement to reach the same reward.
const rewardSaturation = 3.0

// Reward maps the raw engagement counts of a post to a scalar reward in [0,1).
//
// Negative inputs are clamped to zero. The weighted engagement score favours
// replies (weight 3) over reactions (weight 2) over renotes (weight 1), because
// a reply is the strongest signal that a post provoked genuine interaction. The
// score is then passed through a saturating transform score/(score+k) so that
// the reward grows with engagement but never reaches 1, with diminishing
// returns as engagement accumulates. Zero engagement yields exactly 0.
func Reward(reactions, replies, renotes int) float64 {
	if reactions < 0 {
		reactions = 0
	}
	if replies < 0 {
		replies = 0
	}
	if renotes < 0 {
		renotes = 0
	}

	// 返信を最も重く、次いでリアクション、リノートと重み付けする。
	score := float64(2*reactions + 3*replies + 1*renotes)
	if score <= 0 {
		return 0
	}
	return score / (score + rewardSaturation)
}
