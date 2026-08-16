package balancer

import (
	"strconv"
	"strings"
)

// buildKey3 构造格式为 "int:int:string" 的 key，避免 fmt.Sprintf 的堆分配。
// 用于 circuit/cooldown/availability/speed key 生成。
func buildKey3(id1, id2 int, str string) string {
	// 预估容量：两个 int (最多各10位) + 两个冒号 + 字符串长度
	var b strings.Builder
	b.Grow(24 + len(str))
	b.WriteString(strconv.Itoa(id1))
	b.WriteByte(':')
	b.WriteString(strconv.Itoa(id2))
	b.WriteByte(':')
	b.WriteString(str)
	return b.String()
}

// buildKey2 构造格式为 "int:string" 的 key。
// 用于 Auto 策略统计 key 生成。
func buildKey2(id int, str string) string {
	var b strings.Builder
	b.Grow(12 + len(str))
	b.WriteString(strconv.Itoa(id))
	b.WriteByte(':')
	b.WriteString(str)
	return b.String()
}

// buildKeyPrefix 构造格式为 "int:" 的前缀。
// 用于按 channelID 清理时的前缀匹配。
func buildKeyPrefix(id int) string {
	var b strings.Builder
	b.Grow(12)
	b.WriteString(strconv.Itoa(id))
	b.WriteByte(':')
	return b.String()
}
