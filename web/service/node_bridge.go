package service

import (
	"encoding/json"
	"hash/crc32"
	"strings"

	"github.com/alireza0/x-ui/util/common"
)

const (
	nodeBridgeTagPrefix = "node-bridge-"
	nodeBridgePortBase  = 40000
	nodeBridgePortRange = 1000
)

// NodeBridgeService manages the local SOCKS5 "bridge" inbounds + routing rules
// injected into the Xray config template so the Go HTTP client can route
// requests to remote nodes through one of this panel's configured outbounds
// (including vless/vmess), by dialing 127.0.0.1:<bridgePort> as a SOCKS5 proxy.
type NodeBridgeService struct {
	settingService     SettingService
	xraySettingService XraySettingService
	inboundService     InboundService
	xrayService        XrayService
}

func bridgeTag(outboundTag string) string {
	return nodeBridgeTagPrefix + outboundTag
}

// GetOutboundTags returns the tags of all outbounds configured in the Xray
// config template, for populating the "route via outbound" dropdown.
func (s *NodeBridgeService) GetOutboundTags() ([]string, error) {
	templateStr, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}
	var template struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(templateStr), &template); err != nil {
		return nil, common.NewErrorf("invalid xray template config: %v", err)
	}
	tags := make([]string, 0, len(template.Outbounds))
	for _, ob := range template.Outbounds {
		if tag, _ := ob["tag"].(string); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

// GetBridgePort returns the local port of the bridge inbound bound to the
// given outbound tag. EnsureBridges must have been called for this tag first.
func (s *NodeBridgeService) GetBridgePort(outboundTag string) (int, error) {
	templateStr, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return 0, err
	}
	var template struct {
		Inbounds []map[string]interface{} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(templateStr), &template); err != nil {
		return 0, common.NewErrorf("invalid xray template config: %v", err)
	}
	bTag := bridgeTag(outboundTag)
	for _, in := range template.Inbounds {
		if tag, _ := in["tag"].(string); tag == bTag {
			if port, ok := toInt(in["port"]); ok {
				return port, nil
			}
		}
	}
	return 0, common.NewErrorf("bridge inbound %v not found - re-save the node to recreate it", bTag)
}

// EnsureBridges makes sure the Xray config template has exactly one local
// SOCKS5 bridge inbound + routing rule for each tag in outboundTags, removing
// any bridges that are no longer referenced by any node. A bad/incompatible
// template is rejected (via CheckXrayConfig) before anything is saved.
func (s *NodeBridgeService) EnsureBridges(outboundTags []string) error {
	templateStr, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return err
	}

	var template map[string]json.RawMessage
	if err := json.Unmarshal([]byte(templateStr), &template); err != nil {
		return common.NewErrorf("invalid xray template config: %v", err)
	}

	tags := uniqueNonEmpty(outboundTags)

	usedPorts, err := s.usedPorts()
	if err != nil {
		return err
	}

	if err := s.updateInbounds(template, tags, usedPorts); err != nil {
		return err
	}
	if err := s.updateRouting(template, tags); err != nil {
		return err
	}

	newTemplate, err := json.Marshal(template)
	if err != nil {
		return err
	}

	if err := s.xraySettingService.SaveXraySetting(string(newTemplate)); err != nil {
		return err
	}

	s.xrayService.SetToNeedRestart()
	return nil
}

func (s *NodeBridgeService) usedPorts() (map[int]bool, error) {
	inbounds, err := s.inboundService.GetAllInbounds()
	if err != nil {
		return nil, err
	}
	ports := make(map[int]bool, len(inbounds))
	for _, in := range inbounds {
		ports[in.Port] = true
	}
	return ports, nil
}

func (s *NodeBridgeService) updateInbounds(template map[string]json.RawMessage, tags []string, usedPorts map[int]bool) error {
	var inbounds []map[string]interface{}
	if raw, ok := template["inbounds"]; ok {
		if err := json.Unmarshal(raw, &inbounds); err != nil {
			return common.NewErrorf("invalid inbounds in xray template: %v", err)
		}
	}

	existingPorts := map[string]int{}
	kept := make([]map[string]interface{}, 0, len(inbounds))
	for _, in := range inbounds {
		tag, _ := in["tag"].(string)
		if strings.HasPrefix(tag, nodeBridgeTagPrefix) {
			if port, ok := toInt(in["port"]); ok {
				existingPorts[tag] = port
			}
			continue
		}
		kept = append(kept, in)
	}

	chosenPorts := map[int]bool{}
	for _, tag := range tags {
		bTag := bridgeTag(tag)
		port, ok := existingPorts[bTag]
		if !ok || port == 0 || usedPorts[port] || chosenPorts[port] {
			port = allocatePort(bTag, usedPorts, chosenPorts)
		}
		chosenPorts[port] = true
		kept = append(kept, map[string]interface{}{
			"listen":   "127.0.0.1",
			"port":     port,
			"protocol": "socks",
			"tag":      bTag,
			"settings": map[string]interface{}{
				"auth": "noauth",
				"udp":  true,
			},
			"sniffing": map[string]interface{}{
				"enabled": false,
			},
		})
	}

	raw, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	template["inbounds"] = raw
	return nil
}

func (s *NodeBridgeService) updateRouting(template map[string]json.RawMessage, tags []string) error {
	var routing map[string]json.RawMessage
	if raw, ok := template["routing"]; ok {
		if err := json.Unmarshal(raw, &routing); err != nil {
			return common.NewErrorf("invalid routing in xray template: %v", err)
		}
	}
	if routing == nil {
		routing = map[string]json.RawMessage{}
	}

	var rules []map[string]interface{}
	if raw, ok := routing["rules"]; ok {
		if err := json.Unmarshal(raw, &rules); err != nil {
			return common.NewErrorf("invalid routing rules in xray template: %v", err)
		}
	}

	kept := make([]map[string]interface{}, 0, len(rules))
	for _, rule := range rules {
		if isBridgeRule(rule) {
			continue
		}
		kept = append(kept, rule)
	}

	newRules := make([]map[string]interface{}, 0, len(tags)+len(kept))
	for _, tag := range tags {
		newRules = append(newRules, map[string]interface{}{
			"type":        "field",
			"inboundTag":  []string{bridgeTag(tag)},
			"outboundTag": tag,
		})
	}
	newRules = append(newRules, kept...)

	rulesRaw, err := json.Marshal(newRules)
	if err != nil {
		return err
	}
	routing["rules"] = rulesRaw

	routingRaw, err := json.Marshal(routing)
	if err != nil {
		return err
	}
	template["routing"] = routingRaw
	return nil
}

func isBridgeRule(rule map[string]interface{}) bool {
	inboundTags, ok := rule["inboundTag"].([]interface{})
	if !ok {
		return false
	}
	for _, t := range inboundTags {
		if s, ok := t.(string); ok && strings.HasPrefix(s, nodeBridgeTagPrefix) {
			return true
		}
	}
	return false
}

func allocatePort(seed string, usedPorts map[int]bool, chosenPorts map[int]bool) int {
	h := crc32.ChecksumIEEE([]byte(seed))
	base := nodeBridgePortBase + int(h%uint32(nodeBridgePortRange))
	for i := 0; i < nodeBridgePortRange*2; i++ {
		p := base + i
		if !usedPorts[p] && !chosenPorts[p] {
			return p
		}
	}
	return base
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case int:
		return n, true
	}
	return 0, false
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		result = append(result, v)
	}
	return result
}
