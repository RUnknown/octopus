package balancer

import "github.com/bestruirui/octopus/internal/op"

func init() {
	op.RegisterRelayBalancerStateReset(ResetStateByChannel)
}

func ResetStateByChannel(channelID int) {
	resetCircuitBreakerByChannel(channelID)
	resetStickyByChannel(channelID)
	// Auto 策略统计清理：防止 globalAutoStats 随渠道删除而残留
	RemoveChannelStats(channelID)
}
