package sub

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type SUBController struct {
	subPath        string
	subJsonPath    string
	subEncrypt     bool
	updateInterval string

	subService     *SubService
	subJsonService *SubJsonService
}

func NewSUBController(
	g *gin.RouterGroup,
	subPath string,
	jsonPath string,
	encrypt bool,
	showInfo bool,
	rModel string,
	update string,
	jsonFragment string,
	jsonNoise string,
	jsonMux string,
	jsonRules string,
) *SUBController {
	sub := NewSubService(showInfo, rModel)
	a := &SUBController{
		subPath:        subPath,
		subJsonPath:    jsonPath,
		subEncrypt:     encrypt,
		updateInterval: update,

		subService:     sub,
		subJsonService: NewSubJsonService(jsonFragment, jsonNoise, jsonMux, jsonRules, sub),
	}
	a.initRouter(g)
	return a
}

func (a *SUBController) initRouter(g *gin.RouterGroup) {
	gLink := g.Group(a.subPath)
	gJson := g.Group(a.subJsonPath)

	gLink.GET(":subid", a.subs)
	gJson.GET(":subid", a.subJsons)
}

// subs handles subscription requests.
// Browser requests (Accept: text/html) receive a graphical HTML page;
// client apps (v2rayng, clash, …) receive the standard base64 subscription.
func (a *SUBController) subs(c *gin.Context) {
	subId := c.Param("subid")

	host := c.Request.Host
	if colonIndex := strings.LastIndex(host, ":"); colonIndex != -1 {
		host, _, _ = net.SplitHostPort(c.Request.Host)
	}

	subs, header, err := a.subService.GetSubs(subId, host)
	if err != nil {
		// subId not found in DB at all
		c.String(400, "Error!")
		return
	}

	// subs may be empty when the service exists but is currently disabled.
	// We must still respond with 200 + traffic headers so client apps can display
	// quota/expiry info, and browsers see an informative "inactive" page.
	isActive := len(subs) > 0

	// ── Browser detection ─────────────────────────────────────────────────────
	accept    := c.GetHeader("Accept")
	ua        := c.GetHeader("User-Agent")
	isBrowser := strings.Contains(strings.ToLower(accept), "text/html") ||
		c.Query("html") == "1" ||
		strings.Contains(strings.ToLower(ua), "mozilla")

	if isBrowser {
		up, down, total, expireSec := parseTrafficHeader(header)
		subURL := buildSubURL(c, a.subPath, subId)
		remark := extractRemark(subs, subId)
		html   := renderSubHTML(subId, subURL, remark, up, down, total, expireSec, subs, isActive)
		c.Data(200, "text/html; charset=utf-8", []byte(html))
		return
	}

	// ── Client app: standard subscription response ────────────────────────────
	// Return 200 always. When disabled, body is empty — apps show 0 servers but
	// still parse the Subscription-Userinfo header for traffic/expiry display.
	result := ""
	for _, sub := range subs {
		result += sub + "\n"
	}

	c.Writer.Header().Set("Subscription-Userinfo", header)
	c.Writer.Header().Set("Profile-Update-Interval", a.updateInterval)
	c.Writer.Header().Set("Profile-Title", subId)

	if a.subEncrypt && result != "" {
		c.String(200, base64.StdEncoding.EncodeToString([]byte(result)))
	} else {
		c.String(200, result)
	}
}

func (a *SUBController) subJsons(c *gin.Context) {
	subId := c.Param("subid")
	host  := c.Request.Host
	if colonIndex := strings.LastIndex(host, ":"); colonIndex != -1 {
		host, _, _ = net.SplitHostPort(c.Request.Host)
	}
	jsonSub, header, err := a.subJsonService.GetJson(subId, host)
	if err != nil || len(jsonSub) == 0 {
		c.String(400, "Error!")
	} else {
		c.Writer.Header().Set("Subscription-Userinfo", header)
		c.Writer.Header().Set("Profile-Update-Interval", a.updateInterval)
		c.Writer.Header().Set("Profile-Title", subId)
		c.String(200, jsonSub)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// buildSubURL constructs the full subscription URL (scheme://host/subPath/subId).
func buildSubURL(c *gin.Context, subPath, subId string) string {
	scheme := "https"
	if c.Request.TLS == nil {
		scheme = "http"
	}
	host := c.Request.Host
	return fmt.Sprintf("%s://%s%s%s", scheme, host, subPath, subId)
}

// extractRemark tries to get a human-readable name from the subscription links.
// For vless/trojan/ss it uses the #fragment; for vmess it decodes the base64 JSON.
// Falls back to subId if nothing useful is found.
func extractRemark(subs []string, fallback string) string {
	for _, link := range subs {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		// vmess:// → base64 JSON with "ps" field
		if strings.HasPrefix(link, "vmess://") {
			b64 := strings.TrimPrefix(link, "vmess://")
			if idx := strings.Index(b64, "#"); idx != -1 {
				b64 = b64[:idx]
			}
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(b64)
			}
			if err == nil {
				s := string(decoded)
				if i := strings.Index(s, `"ps":"`); i != -1 {
					s = s[i+6:]
					if j := strings.Index(s, `"`); j != -1 {
						if name := strings.TrimSpace(s[:j]); name != "" {
							return name
						}
					}
				}
			}
		}
		// All other protocols: #fragment at the end
		if h := strings.LastIndex(link, "#"); h != -1 {
			if name, err := url.PathUnescape(link[h+1:]); err == nil {
				name = strings.TrimSpace(name)
				if name != "" {
					return name
				}
			}
		}
	}
	return fallback
}

// parseTrafficHeader parses the Subscription-Userinfo header string
// e.g. "upload=1024; download=2048; total=10737418240; expire=1700000000"
// expire is in seconds (0 = no expiry).
func parseTrafficHeader(header string) (up, down, total, expireSec int64) {
	for _, part := range strings.Split(header, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		val, _ := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		switch strings.TrimSpace(kv[0]) {
		case "upload":
			up = val
		case "download":
			down = val
		case "total":
			total = val
		case "expire":
			expireSec = val
		}
	}
	return
}
