package sub

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/util/random"
	"github.com/alireza0/x-ui/web/service"
	"github.com/alireza0/x-ui/xray"

	"github.com/goccy/go-json"
)

type SubService struct {
	address     string
	showInfo    bool
	remarkModel string

	inboundService  service.InboundService
	locationService service.LocationService
	locByInbound    map[int]model.Location // inbound id -> location (for domain/flag overrides)
}

// inboundAddress returns the location's domain for a location inbound (when set),
// otherwise the host the subscription was requested with.
func (s *SubService) inboundAddress(inbound *model.Inbound) string {
	if loc, ok := s.locByInbound[inbound.Id]; ok {
		if d := strings.TrimSpace(loc.Domain); d != "" {
			return d
		}
	}
	return s.address
}

func NewSubService(showInfo bool, remarkModel string) *SubService {
	return &SubService{
		showInfo:    showInfo,
		remarkModel: remarkModel,
	}
}

func (s *SubService) GetSubs(subId string, host string) ([]string, string, error) {
	s.address = host

	// Build the inbound -> location map so location inbounds can use their own
	// domain and flag in the generated links.
	s.locByInbound = map[int]model.Location{}
	if locs, err := s.locationService.GetAllLocations(); err == nil {
		for _, l := range locs {
			s.locByInbound[l.InboundId] = l
		}
	}

	var result []string
	var header string
	var traffic xray.ClientTraffic
	unlimitedTotal := false // true if any inbound has Total==0 (unlimited)
	inbounds, err := s.getInboundsBySubId(subId)
	if err != nil {
		return nil, "", err
	}

	// Prepare Inbounds
	for _, inbound := range inbounds {
		clients, err := s.inboundService.GetClients(inbound)
		if err != nil {
			logger.Error("SubService - GetClients: Unable to get clients from inbound")
		}
		if clients == nil {
			continue
		}
		if len(inbound.Listen) > 0 && inbound.Listen[0] == '@' {
			listen, port, streamSettings, err := s.getFallbackMaster(inbound.Listen, inbound.StreamSettings)
			if err == nil {
				inbound.Listen = listen
				inbound.Port = port
				inbound.StreamSettings = streamSettings
			}
		}

		// Collect proxy links for clients matching this subId.
		// Traffic is accumulated regardless of enable state so the sub page always
		// shows up-to-date quota/expiry even for disabled services.
		// VPN links are only included for enabled clients (disabled ones can't connect anyway).
		for _, client := range clients {
			if client.SubID != subId {
				continue
			}
			if client.Enable {
				link := s.getLink(inbound, client.Email)
				// externalProxy returns newline-separated links as one string — split them
				for _, singleLink := range strings.Split(link, "\n") {
					if trimmed := strings.TrimSpace(singleLink); trimmed != "" {
						result = append(result, trimmed)
					}
				}
			}

			// Accumulate traffic only from master (non-location) inbounds.
			// Location inbounds mirror the master's clients and their traffic is
			// already synced back to the master client, so counting them here would
			// multiply the quota by the number of locations.
			if _, isLocation := s.locByInbound[inbound.Id]; !isLocation {
				ct := s.getClientTraffics(inbound.ClientStats, client.Email)
				traffic.Up += ct.Up
				traffic.Down += ct.Down
				if ct.Total == 0 {
					unlimitedTotal = true
				} else if !unlimitedTotal {
					traffic.Total += ct.Total
				}
				if ct.ExpiryTime > 0 {
					if traffic.ExpiryTime == 0 || ct.ExpiryTime < traffic.ExpiryTime {
						traffic.ExpiryTime = ct.ExpiryTime
					}
				}
			}
		}
	}

	// Generate location links: for every enabled location, build links using
	// the location inbound's infrastructure (port/stream) + each matching
	// master client's credentials. Location inbounds have no clients in the
	// DB — they are virtual.
	for locInboundId, loc := range s.locByInbound {
		if !loc.Enable {
			continue
		}
		locInbound, err := s.inboundService.GetInbound(locInboundId)
		if err != nil || !locInbound.Enable {
			continue
		}
		if len(locInbound.Listen) > 0 && locInbound.Listen[0] == '@' {
			listen, port, streamSettings, err := s.getFallbackMaster(locInbound.Listen, locInbound.StreamSettings)
			if err == nil {
				locInbound.Listen = listen
				locInbound.Port = port
				locInbound.StreamSettings = streamSettings
			}
		}
		for _, inbound := range inbounds {
			if _, isLoc := s.locByInbound[inbound.Id]; isLoc {
				continue
			}
			clients, err := s.inboundService.GetClients(inbound)
			if err != nil {
				continue
			}
			for _, client := range clients {
				if client.SubID != subId || !client.Enable {
					continue
				}
				// Build a temporary inbound with location config + master client
				tmpInbound := *locInbound
				tmpSettings := map[string]interface{}{}
				json.Unmarshal([]byte(tmpInbound.Settings), &tmpSettings)
				tmpSettings["clients"] = []model.Client{client}
				newSettings, _ := json.Marshal(tmpSettings)
				tmpInbound.Settings = string(newSettings)

				link := s.getLink(&tmpInbound, client.Email)
				for _, singleLink := range strings.Split(link, "\n") {
					if trimmed := strings.TrimSpace(singleLink); trimmed != "" {
						result = append(result, trimmed)
					}
				}
			}
		}
	}

	totalForHeader := traffic.Total
	if unlimitedTotal {
		totalForHeader = 0 // signal unlimited to VPN apps and sub page
	}
	header = fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d",
		traffic.Up, traffic.Down, totalForHeader, traffic.ExpiryTime/1000)

	// Append remaining traffic and/or days to every link's #fragment.
	var linkSuffix string
	if !unlimitedTotal && traffic.Total > 0 {
		remaining := traffic.Total - traffic.Up - traffic.Down
		if remaining < 0 {
			remaining = 0
		}
		linkSuffix += "-📊" + common.FormatTraffic(remaining)
	}
	if traffic.ExpiryTime > 0 {
		remainingDays := (traffic.ExpiryTime - time.Now().UnixMilli()) / (1000 * 60 * 60 * 24)
		if remainingDays < 0 {
			remainingDays = 0
		}
		linkSuffix += fmt.Sprintf("-%d Days", remainingDays)
	}
	if linkSuffix != "" {
		for i, link := range result {
			if h := strings.LastIndex(link, "#"); h != -1 {
				result[i] = link + linkSuffix
			} else {
				result[i] = link + "#" + linkSuffix[1:]
			}
		}
	}

	return result, header, nil
}

func (s *SubService) getInboundsBySubId(subId string) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("ClientStats").Where(`id in (
		SELECT DISTINCT inbounds.id
		FROM inbounds,
			JSON_EACH(JSON_EXTRACT(inbounds.settings, '$.clients')) AS client
		WHERE
			protocol in ('vmess','vless','trojan','shadowsocks')
			AND JSON_EXTRACT(client.value, '$.subId') = ?
	)`, subId).Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (s *SubService) getClientTraffics(traffics []xray.ClientTraffic, email string) xray.ClientTraffic {
	for _, traffic := range traffics {
		if traffic.Email == email {
			return traffic
		}
	}
	return xray.ClientTraffic{}
}

func (s *SubService) getFallbackMaster(dest string, streamSettings string) (string, int, string, error) {
	db := database.GetDB()
	var inbound *model.Inbound
	err := db.Model(model.Inbound{}).
		Where("JSON_TYPE(settings, '$.fallbacks') = 'array'").
		Where("EXISTS (SELECT * FROM json_each(settings, '$.fallbacks') WHERE json_extract(value, '$.dest') = ?)", dest).
		Find(&inbound).Error
	if err != nil {
		return "", 0, "", err
	}

	var stream map[string]interface{}
	json.Unmarshal([]byte(streamSettings), &stream)
	var masterStream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &masterStream)
	stream["security"] = masterStream["security"]
	stream["tlsSettings"] = masterStream["tlsSettings"]
	stream["externalProxy"] = masterStream["externalProxy"]
	modifiedStream, _ := json.MarshalIndent(stream, "", "  ")

	return inbound.Listen, inbound.Port, string(modifiedStream), nil
}

func (s *SubService) getLink(inbound *model.Inbound, email string) string {
	switch inbound.Protocol {
	case "vmess":
		return s.genVmessLink(inbound, email)
	case "vless":
		return s.genVlessLink(inbound, email)
	case "trojan":
		return s.genTrojanLink(inbound, email)
	case "shadowsocks":
		return s.genShadowsocksLink(inbound, email)
	}
	return ""
}

func (s *SubService) genVmessLink(inbound *model.Inbound, email string) string {
	if inbound.Protocol != model.VMess {
		return ""
	}
	obj := map[string]interface{}{
		"v":    "2",
		"add":  s.inboundAddress(inbound),
		"port": inbound.Port,
		"type": "none",
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	network, _ := stream["network"].(string)
	obj["net"] = network
	switch network {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		obj["type"] = typeStr
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			obj["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		obj["type"], _ = header["type"].(string)
		obj["path"], _ = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		obj["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			obj["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		obj["path"], _ = grpc["serviceName"].(string)
		obj["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			obj["type"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		obj["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			obj["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		obj["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			obj["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			obj["host"] = searchHost(headers)
		}
		obj["mode"] = xhttp["mode"].(string)
	}

	security, _ := stream["security"].(string)
	obj["tls"] = security
	if security == "tls" {
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		if len(alpns) > 0 {
			var alpn []string
			for _, a := range alpns {
				alpn = append(alpn, a.(string))
			}
			obj["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			obj["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				obj["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				obj["allowInsecure"], _ = insecure.(bool)
			}
		}
	}

	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	obj["id"] = clients[clientIndex].ID

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			newObj := map[string]interface{}{}
			for key, value := range obj {
				if !(newSecurity == "none" && (key == "alpn" || key == "sni" || key == "fp" || key == "allowInsecure")) {
					newObj[key] = value
				}
			}
			newObj["ps"] = s.genRemark(inbound, email, ep["remark"].(string))
			newObj["add"] = ep["dest"].(string)
			newObj["port"] = int(ep["port"].(float64))

			if newSecurity != "same" {
				newObj["tls"] = newSecurity
			}
			if index > 0 {
				links += "\n"
			}
			jsonStr, _ := json.MarshalIndent(newObj, "", "  ")
			links += "vmess://" + base64.StdEncoding.EncodeToString(jsonStr)
		}
		return links
	}

	obj["ps"] = s.genRemark(inbound, email, "")

	jsonStr, _ := json.MarshalIndent(obj, "", "  ")
	return "vmess://" + base64.StdEncoding.EncodeToString(jsonStr)
}

func (s *SubService) genVlessLink(inbound *model.Inbound, email string) string {
	address := s.inboundAddress(inbound)
	if inbound.Protocol != model.VLESS {
		return ""
	}
	var vlessSettings model.VLESSSettings
	_ = json.Unmarshal([]byte(inbound.Settings), &vlessSettings)

	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	uuid := clients[clientIndex].ID
	port := inbound.Port
	streamNetwork := stream["network"].(string)
	params := make(map[string]string)
	if vlessSettings.Encryption != "" {
		params["encryption"] = vlessSettings.Encryption
	}
	params["type"] = streamNetwork

	switch streamNetwork {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			params["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
			params["headerType"] = "http"
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		params["headerType"] = header["type"].(string)
		params["seed"] = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		params["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		params["serviceName"] = grpc["serviceName"].(string)
		params["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			params["mode"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		params["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		params["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
		params["mode"] = xhttp["mode"].(string)
	}
	security, _ := stream["security"].(string)
	if security == "tls" {
		params["security"] = "tls"
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		var alpn []string
		for _, a := range alpns {
			alpn = append(alpn, a.(string))
		}
		if len(alpn) > 0 {
			params["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			params["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				params["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				if insecure.(bool) {
					params["allowInsecure"] = "1"
				}
			}
		}

		if streamNetwork == "tcp" && len(clients[clientIndex].Flow) > 0 {
			params["flow"] = clients[clientIndex].Flow
		}
	}

	if security == "reality" {
		params["security"] = "reality"
		realitySetting, _ := stream["realitySettings"].(map[string]interface{})
		realitySettings, _ := searchKey(realitySetting, "settings")
		if realitySetting != nil {
			if sniValue, ok := searchKey(realitySetting, "serverNames"); ok {
				sNames, _ := sniValue.([]interface{})
				params["sni"] = sNames[random.Num(len(sNames))].(string)
			}
			if pbkValue, ok := searchKey(realitySettings, "publicKey"); ok {
				params["pbk"], _ = pbkValue.(string)
			}
			if sidValue, ok := searchKey(realitySetting, "shortIds"); ok {
				shortIds, _ := sidValue.([]interface{})
				params["sid"] = shortIds[random.Num(len(shortIds))].(string)
			}
			if fpValue, ok := searchKey(realitySettings, "fingerprint"); ok {
				if fp, ok := fpValue.(string); ok && len(fp) > 0 {
					params["fp"] = fp
				}
			}
			if pqvValue, ok := searchKey(realitySettings, "mldsa65Verify"); ok {
				if pqv, ok := pqvValue.(string); ok && len(pqv) > 0 {
					params["pqv"] = pqv
				}
			}
			params["spx"] = "/" + random.Seq(15)
		}

		if streamNetwork == "tcp" && len(clients[clientIndex].Flow) > 0 {
			params["flow"] = clients[clientIndex].Flow
		}
	}

	if security != "tls" && security != "reality" {
		params["security"] = "none"
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			link := fmt.Sprintf("vless://%s@%s:%d", uuid, dest, port)

			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = security
			}
			url, _ := url.Parse(link)
			q := url.Query()

			for k, v := range params {
				if !(newSecurity == "none" && (k == "alpn" || k == "sni" || k == "fp" || k == "allowInsecure")) {
					q.Add(k, v)
				}
			}

			// Set the new query values on the URL
			url.RawQuery = q.Encode()

			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))

			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("vless://%s@%s:%d", uuid, address, port)
	url, _ := url.Parse(link)
	q := url.Query()

	for k, v := range params {
		q.Add(k, v)
	}

	// Set the new query values on the URL
	url.RawQuery = q.Encode()

	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genTrojanLink(inbound *model.Inbound, email string) string {
	address := s.inboundAddress(inbound)
	if inbound.Protocol != model.Trojan {
		return ""
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	password := clients[clientIndex].Password
	port := inbound.Port
	streamNetwork := stream["network"].(string)
	params := make(map[string]string)
	params["type"] = streamNetwork

	switch streamNetwork {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			params["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
			params["headerType"] = "http"
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		params["headerType"] = header["type"].(string)
		params["seed"] = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		params["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		params["serviceName"] = grpc["serviceName"].(string)
		params["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			params["mode"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		params["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		params["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
		params["mode"] = xhttp["mode"].(string)
	}
	security, _ := stream["security"].(string)
	if security == "tls" {
		params["security"] = "tls"
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		var alpn []string
		for _, a := range alpns {
			alpn = append(alpn, a.(string))
		}
		if len(alpn) > 0 {
			params["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			params["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				params["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				if insecure.(bool) {
					params["allowInsecure"] = "1"
				}
			}
		}
	}

	if security == "reality" {
		params["security"] = "reality"
		realitySetting, _ := stream["realitySettings"].(map[string]interface{})
		realitySettings, _ := searchKey(realitySetting, "settings")
		if realitySetting != nil {
			if sniValue, ok := searchKey(realitySetting, "serverNames"); ok {
				sNames, _ := sniValue.([]interface{})
				params["sni"] = sNames[random.Num(len(sNames))].(string)
			}
			if pbkValue, ok := searchKey(realitySettings, "publicKey"); ok {
				params["pbk"], _ = pbkValue.(string)
			}
			if sidValue, ok := searchKey(realitySetting, "shortIds"); ok {
				shortIds, _ := sidValue.([]interface{})
				params["sid"] = shortIds[random.Num(len(shortIds))].(string)
			}
			if fpValue, ok := searchKey(realitySettings, "fingerprint"); ok {
				if fp, ok := fpValue.(string); ok && len(fp) > 0 {
					params["fp"] = fp
				}
			}
			if pqvValue, ok := searchKey(realitySettings, "mldsa65Verify"); ok {
				if pqv, ok := pqvValue.(string); ok && len(pqv) > 0 {
					params["pqv"] = pqv
				}
			}
			params["spx"] = "/" + random.Seq(15)
		}
	}

	if security != "tls" && security != "reality" {
		params["security"] = "none"
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			link := fmt.Sprintf("trojan://%s@%s:%d", password, dest, port)

			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = security
			}
			url, _ := url.Parse(link)
			q := url.Query()

			for k, v := range params {
				if !(newSecurity == "none" && (k == "alpn" || k == "sni" || k == "fp" || k == "allowInsecure")) {
					q.Add(k, v)
				}
			}

			// Set the new query values on the URL
			url.RawQuery = q.Encode()

			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))

			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("trojan://%s@%s:%d", password, address, port)

	url, _ := url.Parse(link)
	q := url.Query()

	for k, v := range params {
		q.Add(k, v)
	}

	// Set the new query values on the URL
	url.RawQuery = q.Encode()

	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genShadowsocksLink(inbound *model.Inbound, email string) string {
	address := s.inboundAddress(inbound)
	if inbound.Protocol != model.Shadowsocks {
		return ""
	}
	var stream map[string]interface{}
	json.Unmarshal([]byte(inbound.StreamSettings), &stream)
	clients, _ := s.inboundService.GetClients(inbound)

	var settings map[string]interface{}
	json.Unmarshal([]byte(inbound.Settings), &settings)
	inboundPassword := settings["password"].(string)
	method := settings["method"].(string)
	clientIndex := -1
	for i, client := range clients {
		if client.Email == email {
			clientIndex = i
			break
		}
	}
	streamNetwork := stream["network"].(string)
	params := make(map[string]string)
	params["type"] = streamNetwork

	switch streamNetwork {
	case "tcp":
		tcp, _ := stream["tcpSettings"].(map[string]interface{})
		header, _ := tcp["header"].(map[string]interface{})
		typeStr, _ := header["type"].(string)
		if typeStr == "http" {
			request := header["request"].(map[string]interface{})
			requestPath, _ := request["path"].([]interface{})
			params["path"] = requestPath[0].(string)
			headers, _ := request["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
			params["headerType"] = "http"
		}
	case "kcp":
		kcp, _ := stream["kcpSettings"].(map[string]interface{})
		header, _ := kcp["header"].(map[string]interface{})
		params["headerType"] = header["type"].(string)
		params["seed"] = kcp["seed"].(string)
	case "ws":
		ws, _ := stream["wsSettings"].(map[string]interface{})
		params["path"] = ws["path"].(string)
		if host, ok := ws["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := ws["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "grpc":
		grpc, _ := stream["grpcSettings"].(map[string]interface{})
		params["serviceName"] = grpc["serviceName"].(string)
		params["authority"], _ = grpc["authority"].(string)
		if grpc["multiMode"].(bool) {
			params["mode"] = "multi"
		}
	case "httpupgrade":
		httpupgrade, _ := stream["httpupgradeSettings"].(map[string]interface{})
		params["path"] = httpupgrade["path"].(string)
		if host, ok := httpupgrade["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := httpupgrade["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
	case "xhttp":
		xhttp, _ := stream["xhttpSettings"].(map[string]interface{})
		params["path"] = xhttp["path"].(string)
		if host, ok := xhttp["host"].(string); ok && len(host) > 0 {
			params["host"] = host
		} else {
			headers, _ := xhttp["headers"].(map[string]interface{})
			params["host"] = searchHost(headers)
		}
		params["mode"] = xhttp["mode"].(string)
	}

	security, _ := stream["security"].(string)
	if security == "tls" {
		params["security"] = "tls"
		tlsSetting, _ := stream["tlsSettings"].(map[string]interface{})
		alpns, _ := tlsSetting["alpn"].([]interface{})
		var alpn []string
		for _, a := range alpns {
			alpn = append(alpn, a.(string))
		}
		if len(alpn) > 0 {
			params["alpn"] = strings.Join(alpn, ",")
		}
		if sniValue, ok := searchKey(tlsSetting, "serverName"); ok {
			params["sni"], _ = sniValue.(string)
		}

		tlsSettings, _ := searchKey(tlsSetting, "settings")
		if tlsSetting != nil {
			if fpValue, ok := searchKey(tlsSettings, "fingerprint"); ok {
				params["fp"], _ = fpValue.(string)
			}
			if insecure, ok := searchKey(tlsSettings, "allowInsecure"); ok {
				if insecure.(bool) {
					params["allowInsecure"] = "1"
				}
			}
		}
	}

	encPart := fmt.Sprintf("%s:%s", method, clients[clientIndex].Password)
	if method[0] == '2' {
		encPart = fmt.Sprintf("%s:%s:%s", method, inboundPassword, clients[clientIndex].Password)
	}

	externalProxies, _ := stream["externalProxy"].([]interface{})

	if len(externalProxies) > 0 {
		links := ""
		for index, externalProxy := range externalProxies {
			ep, _ := externalProxy.(map[string]interface{})
			newSecurity, _ := ep["forceTls"].(string)
			dest, _ := ep["dest"].(string)
			port := int(ep["port"].(float64))
			link := fmt.Sprintf("ss://%s@%s:%d", base64.StdEncoding.EncodeToString([]byte(encPart)), dest, port)

			if newSecurity != "same" {
				params["security"] = newSecurity
			} else {
				params["security"] = security
			}
			url, _ := url.Parse(link)
			q := url.Query()

			for k, v := range params {
				if !(newSecurity == "none" && (k == "alpn" || k == "sni" || k == "fp" || k == "allowInsecure")) {
					q.Add(k, v)
				}
			}

			// Set the new query values on the URL
			url.RawQuery = q.Encode()

			url.Fragment = s.genRemark(inbound, email, ep["remark"].(string))

			if index > 0 {
				links += "\n"
			}
			links += url.String()
		}
		return links
	}

	link := fmt.Sprintf("ss://%s@%s:%d", base64.StdEncoding.EncodeToString([]byte(encPart)), address, inbound.Port)
	url, _ := url.Parse(link)
	q := url.Query()

	for k, v := range params {
		q.Add(k, v)
	}

	// Set the new query values on the URL
	url.RawQuery = q.Encode()

	url.Fragment = s.genRemark(inbound, email, "")
	return url.String()
}

func (s *SubService) genRemark(inbound *model.Inbound, email string, extra string) string {
	name := email

	// When an external proxy supplies a remark, use it as a template. Supported
	// variables: {clientname} / {email} -> the client's email, {inbound} -> the
	// inbound remark. e.g. "amir-{clientname}" => "amir-<email>".
	if extra != "" {
		name = strings.ReplaceAll(extra, "{clientname}", email)
		name = strings.ReplaceAll(name, "{email}", email)
		name = strings.ReplaceAll(name, "{inbound}", inbound.Remark)
	}

	// For a location inbound, label the entry with the country flag + location
	// remark, e.g. "🇹🇷turkey" (flag prefixes any external-proxy template too).
	if loc, ok := s.locByInbound[inbound.Id]; ok {
		base := loc.Remark
		if extra != "" {
			base = name
		}
		name = loc.Flag + base
	} else if inbound.Flag != "" {
		name = inbound.Flag + name
	}

	if !s.showInfo {
		return name
	}

	ct := s.getClientTraffics(inbound.ClientStats, email)
	remark := name
	if ct.Total > 0 {
		remaining := ct.Total - ct.Up - ct.Down
		if remaining < 0 {
			remaining = 0
		}
		remark += "-" + common.FormatTraffic(remaining) + "📊"
	}
	if ct.ExpiryTime > 0 {
		remainingDays := (ct.ExpiryTime - time.Now().UnixMilli()) / (1000 * 60 * 60 * 24)
		if remainingDays < 0 {
			remainingDays = 0
		}
		remark += fmt.Sprintf("-%d Days", remainingDays)
	}
	return remark
}

func searchKey(data interface{}, key string) (interface{}, bool) {
	switch val := data.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if k == key {
				return v, true
			}
			if result, ok := searchKey(v, key); ok {
				return result, true
			}
		}
	case []interface{}:
		for _, v := range val {
			if result, ok := searchKey(v, key); ok {
				return result, true
			}
		}
	}
	return nil, false
}

func searchHost(headers interface{}) string {
	data, _ := headers.(map[string]interface{})
	for k, v := range data {
		if strings.EqualFold(k, "host") {
			switch v.(type) {
			case []interface{}:
				hosts, _ := v.([]interface{})
				if len(hosts) > 0 {
					return hosts[0].(string)
				} else {
					return ""
				}
			case interface{}:
				return v.(string)
			}
		}
	}

	return ""
}
