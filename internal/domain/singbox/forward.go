package singbox

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// ForwardInput describes one managed layer-4 port forward rendered as a
// sing-box `direct` inbound plus an explicit top-level route rule.
type ForwardInput struct {
	Tag             string
	ListenPort      int
	Network         string // "", "tcp", "udp"
	OverrideAddress string
	OverridePort    int
	Outbound        string // default "direct"
}

type forwardImport struct {
	inboundIndex int
	ruleIndex    int
	forward      ForwardInput
}

// NormalizeForwardNetwork canonicalizes the forward protocol filter (`""`, `tcp`, or `udp`).
func NormalizeForwardNetwork(network string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(network))
	switch n {
	case "", "tcp", "udp":
		return n, nil
	default:
		return "", fmt.Errorf("forward network must be empty, tcp, or udp (got %q)", network)
	}
}

// ValidateForwardTargetSyntax ensures the target host is a valid non-empty IP or hostname
// without whitespace, URL schemes, paths, or an embedded port.
func ValidateForwardTargetSyntax(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", fmt.Errorf("forward target address is required")
	}
	if strings.ContainsAny(trimmed, " \t\r\n/\\") || strings.Contains(trimmed, "://") {
		return "", fmt.Errorf("forward target address %q contains invalid characters", addr)
	}
	if _, err := netip.ParseAddr(trimmed); err == nil {
		return trimmed, nil
	}
	// Strip optional bracketed IPv6 if a caller wrapped a literal address.
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		inner := trimmed[1 : len(trimmed)-1]
		if _, err := netip.ParseAddr(inner); err == nil {
			return inner, nil
		}
	}
	if strings.Contains(trimmed, ":") {
		return "", fmt.Errorf("forward target address %q must be a host/IP without port", addr)
	}
	return trimmed, nil
}

// IsDisallowedForwardTarget rejects loopback, unspecified, link-local (including cloud
// metadata 169.254.0.0/16), and multicast destinations to prevent local/host SSRF.
func IsDisallowedForwardTarget(addr string) bool {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		trimmed = trimmed[1 : len(trimmed)-1]
	}
	if ip, err := netip.ParseAddr(trimmed); err == nil {
		ip = ip.Unmap()
		return ip.IsLoopback() ||
			ip.IsUnspecified() ||
			ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() ||
			ip.IsInterfaceLocalMulticast() ||
			ip.IsMulticast()
	}
	lower := strings.TrimSuffix(strings.ToLower(trimmed), ".")
	if lower == "localhost" ||
		strings.HasSuffix(lower, ".localhost") ||
		lower == "localhost.localdomain" ||
		lower == "ip6-localhost" ||
		lower == "ip6-loopback" ||
		lower == "metadata.google.internal" {
		return true
	}
	return false
}

func forwardEffectiveOutbound(outbound string) string {
	trimmed := strings.TrimSpace(outbound)
	if trimmed == "" {
		return "direct"
	}
	return trimmed
}

// ValidateForwardInput validates a single port-forward definition.
func ValidateForwardInput(f ForwardInput) error {
	if strings.TrimSpace(f.Tag) == "" {
		return fmt.Errorf("forward tag is required")
	}
	if f.ListenPort < 1 || f.ListenPort > 65535 {
		return fmt.Errorf("forward %q: listen_port must be between 1 and 65535", f.Tag)
	}
	if _, err := NormalizeForwardNetwork(f.Network); err != nil {
		return fmt.Errorf("forward %q: %w", f.Tag, err)
	}
	if _, err := ValidateForwardTargetSyntax(f.OverrideAddress); err != nil {
		return fmt.Errorf("forward %q: %w", f.Tag, err)
	}
	if f.OverridePort < 1 || f.OverridePort > 65535 {
		return fmt.Errorf("forward %q: override_port must be between 1 and 65535", f.Tag)
	}
	ob := forwardEffectiveOutbound(f.Outbound)
	if ob == "block" || ob == "reject" || ob == "sniff" || ob == "hijack-dns" {
		return fmt.Errorf("forward %q: outbound cannot be %q", f.Tag, ob)
	}
	return nil
}

func inboundGuardTag(in InboundInput) string {
	g := in.Settings.TLS.Reality.Guard
	if g == nil || !g.Enabled {
		return ""
	}
	if t := strings.TrimSpace(g.Tag); t != "" {
		return t
	}
	return in.Tag + "-guard"
}

func reservedStatsPort(addr string) int {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return 0
	}
	_, portStr, err := net.SplitHostPort(trimmed)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p < 1 || p > 65535 {
		return 0
	}
	return p
}

// ValidateForwardLayout ensures all port forwards are valid and do not collide
// on tags or listen ports with inbounds, Reality guards, reserved stats ports,
// or other forwards.
func ValidateForwardLayout(inbounds []InboundInput, forwards []ForwardInput, statsAddresses ...string) error {
	usedTags := make(map[string]string, len(inbounds)*2+len(forwards))
	usedPorts := make(map[int]string, len(inbounds)*2+len(forwards)+len(statsAddresses))

	for _, addr := range statsAddresses {
		if p := reservedStatsPort(addr); p > 0 {
			usedPorts[p] = fmt.Sprintf("stats endpoint %s", addr)
		}
	}
	for _, in := range inbounds {
		if in.Tag != "" {
			usedTags[in.Tag] = fmt.Sprintf("inbound %q", in.Tag)
		}
		if in.ListenPort > 0 {
			usedPorts[in.ListenPort] = fmt.Sprintf("inbound %q", in.Tag)
		}
		if g := in.Settings.TLS.Reality.Guard; g != nil && g.Enabled {
			if gt := inboundGuardTag(in); gt != "" {
				usedTags[gt] = fmt.Sprintf("reality guard %q", gt)
			}
			if g.InternalPort > 0 {
				usedPorts[g.InternalPort] = fmt.Sprintf("reality guard internal port of %q", in.Tag)
			}
		}
	}

	for _, f := range forwards {
		if err := ValidateForwardInput(f); err != nil {
			return err
		}
		tag := strings.TrimSpace(f.Tag)
		if owner, exists := usedTags[tag]; exists {
			return fmt.Errorf("forward tag %q conflicts with %s", tag, owner)
		}
		if owner, exists := usedPorts[f.ListenPort]; exists {
			return fmt.Errorf("forward %q listen_port %d conflicts with %s", tag, f.ListenPort, owner)
		}
		usedTags[tag] = fmt.Sprintf("forward %q", tag)
		usedPorts[f.ListenPort] = fmt.Sprintf("forward %q", tag)
	}
	return nil
}

// BuildForwardInbound builds the sing-box `direct` inbound JSON for a PortForward.
func BuildForwardInbound(f ForwardInput) (json.RawMessage, error) {
	if err := ValidateForwardInput(f); err != nil {
		return nil, err
	}
	netProto, err := NormalizeForwardNetwork(f.Network)
	if err != nil {
		return nil, err
	}
	targetAddr, err := ValidateForwardTargetSyntax(f.OverrideAddress)
	if err != nil {
		return nil, err
	}
	obj := map[string]any{
		"type":             "direct",
		"tag":              strings.TrimSpace(f.Tag),
		"listen":           "::",
		"listen_port":      f.ListenPort,
		"override_address": targetAddr,
		"override_port":    f.OverridePort,
	}
	if netProto != "" {
		obj["network"] = netProto
	}
	return json.Marshal(obj)
}

// BuildForwardRule builds the explicit route rule assigning a PortForward inbound
// to its configured outbound.
func BuildForwardRule(f ForwardInput) map[string]any {
	return map[string]any{
		"inbound":  []string{strings.TrimSpace(f.Tag)},
		"action":   "route",
		"outbound": forwardEffectiveOutbound(f.Outbound),
	}
}

func isSimpleForwardRule(rule map[string]any) (tag string, outbound string, ok bool) {
	if !guardOnlyKeys(rule, "inbound", "outbound", "action") {
		return "", "", false
	}
	act := mStr(rule, "action")
	if act != "" && act != "route" {
		return "", "", false
	}
	ob := strings.TrimSpace(mStr(rule, "outbound"))
	if ob == "" || ob == "block" || ob == "reject" || ob == "sniff" || ob == "hijack-dns" {
		return "", "", false
	}
	ibs, ok := guardStringArray(rule["inbound"])
	if !ok || len(ibs) != 1 || strings.TrimSpace(ibs[0]) == "" {
		return "", "", false
	}
	return ibs[0], ob, true
}

func ruleMatchesAllInboundsNonSniff(rule map[string]any) bool {
	if IsLoopbackGuardRule(rule) {
		return false
	}
	if mStr(rule, "action") == "sniff" {
		return false
	}
	ibs, hasInbounds := guardStringArray(rule["inbound"])
	return !hasInbounds || len(ibs) == 0
}

// findForwardImports recovers managed-compatible port forwards from non-guard `direct`
// inbounds and their dedicated route rules inside ParseServerConfig.
func findForwardImports(root map[string]any, guardInbounds, guardRules map[int]bool) ([]forwardImport, map[int]string) {
	inbounds := mSlice(root, "inbounds")
	rules := mSlice(mMap(root, "route"), "rules")
	defaultFinal := strings.TrimSpace(mStr(mMap(root, "route"), "final"))
	if defaultFinal == "" {
		defaultFinal = "direct"
	}

	tagCounts := make(map[string]int, len(inbounds))
	for _, value := range inbounds {
		if m, ok := value.(map[string]any); ok {
			if t := mStr(m, "tag"); t != "" {
				tagCounts[t]++
			}
		}
	}

	var found []forwardImport
	skippedNotes := make(map[int]string)

	for index, value := range inbounds {
		if guardInbounds[index] {
			continue
		}
		m, ok := value.(map[string]any)
		if !ok || mStr(m, "type") != "direct" {
			continue
		}
		tag := strings.TrimSpace(mStr(m, "tag"))
		if tag == "" || tagCounts[tag] != 1 {
			skippedNotes[index] = fmt.Sprintf("入站 %s: direct 入站标签缺失或重复", tag)
			continue
		}
		if !guardOnlyKeys(m, "type", "tag", "listen", "listen_port", "network", "override_address", "override_port") {
			skippedNotes[index] = fmt.Sprintf("入站 %s: direct 入站包含面板暂不支持的高级字段", tag)
			continue
		}
		listen := strings.TrimSpace(mStr(m, "listen"))
		if listen != "" && listen != "::" && listen != "0.0.0.0" {
			skippedNotes[index] = fmt.Sprintf("入站 %s: 端口转发仅支持监听 :: / 0.0.0.0（当前 listen=%s）", tag, listen)
			continue
		}
		listenPort := guardPort(m, "listen_port")
		overridePort := guardPort(m, "override_port")
		overrideAddr := strings.TrimSpace(mStr(m, "override_address"))
		netProto, netErr := NormalizeForwardNetwork(mStr(m, "network"))
		if listenPort == 0 || overridePort == 0 || overrideAddr == "" || netErr != nil {
			skippedNotes[index] = fmt.Sprintf("入站 %s: direct 端口转发缺少有效的 listen_port / override_address / override_port 或协议不受支持", tag)
			continue
		}
		if _, err := ValidateForwardTargetSyntax(overrideAddr); err != nil {
			skippedNotes[index] = fmt.Sprintf("入站 %s: direct 目标地址 %q 格式无效", tag, overrideAddr)
			continue
		}

		// Ensure nothing outside route.rules references this direct inbound tag.
		protected := map[string]bool{tag: true}
		externalRef := false
		for idx2, other := range inbounds {
			if idx2 != index && guardReferences(other, protected) {
				externalRef = true
				break
			}
		}
		if !externalRef {
			for key, val := range root {
				if key != "inbounds" && key != "route" && guardReferences(val, protected) {
					externalRef = true
					break
				}
			}
		}
		if !externalRef {
			for key, val := range mMap(root, "route") {
				if key != "rules" && guardReferences(val, protected) {
					externalRef = true
					break
				}
			}
		}
		if externalRef {
			skippedNotes[index] = fmt.Sprintf("入站 %s: direct 入站被其他高级配置引用", tag)
			continue
		}

		// Inspect route rules targeting `tag`.
		matchingRules := make([]int, 0, 1)
		earlierGlobalBlock := false
		anyGlobalBlock := false
		simpleRuleIdx := -1
		outbound := ""
		complexRule := false

		for rIdx, rVal := range rules {
			if guardRules[rIdx] {
				continue
			}
			rm, ok := rVal.(map[string]any)
			if !ok {
				continue
			}
			if ruleMatchesAllInboundsNonSniff(rm) {
				anyGlobalBlock = true
			}
			if !guardReferences(rm, protected) {
				continue
			}
			matchingRules = append(matchingRules, rIdx)
			rTag, rOut, simple := isSimpleForwardRule(rm)
			if !simple || rTag != tag {
				complexRule = true
				break
			}
			simpleRuleIdx = rIdx
			outbound = rOut
		}

		if complexRule || len(matchingRules) > 1 {
			skippedNotes[index] = fmt.Sprintf("入站 %s: direct 入站关联了复杂或多条路由规则", tag)
			continue
		}
		if simpleRuleIdx >= 0 {
			for rIdx := 0; rIdx < simpleRuleIdx; rIdx++ {
				if guardRules[rIdx] {
					continue
				}
				if rm, ok := rules[rIdx].(map[string]any); ok && ruleMatchesAllInboundsNonSniff(rm) {
					earlierGlobalBlock = true
					break
				}
			}
			if earlierGlobalBlock {
				skippedNotes[index] = fmt.Sprintf("入站 %s: 转发规则之前存在全局分流或拦截规则，已跳过", tag)
				continue
			}
		} else {
			if anyGlobalBlock {
				skippedNotes[index] = fmt.Sprintf("入站 %s: 无专属转发规则且存在全局分流规则，已跳过", tag)
				continue
			}
			if defaultFinal == "block" || defaultFinal == "reject" {
				skippedNotes[index] = fmt.Sprintf("入站 %s: 默认出站为拦截动作，已跳过", tag)
				continue
			}
			outbound = defaultFinal
		}

		found = append(found, forwardImport{
			inboundIndex: index,
			ruleIndex:    simpleRuleIdx,
			forward: ForwardInput{
				Tag:             tag,
				ListenPort:      listenPort,
				Network:         netProto,
				OverrideAddress: overrideAddr,
				OverridePort:    overridePort,
				Outbound:        outbound,
			},
		})
	}
	return found, skippedNotes
}
