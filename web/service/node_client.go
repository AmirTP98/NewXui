package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/web/entity"
	"github.com/alireza0/x-ui/xray"

	"golang.org/x/net/proxy"
)

// NodeClient talks to a remote x-ui panel's /login and /xui/API/inbounds/* endpoints.
type NodeClient struct {
	node     *model.Node
	client   *http.Client
	baseUrl  string
	loggedIn bool
}

// NewNodeClient builds an http.Client whose transport matches the node's ProxyMode.
func NewNodeClient(node *model.Node) (*NodeClient, error) {
	transport := &http.Transport{}

	switch node.ProxyMode {
	case model.ProxyModeProxyUrl:
		t, err := buildProxyURLTransport(node.ProxyUrl)
		if err != nil {
			return nil, common.NewErrorf("invalid proxy url for node %v: %v", node.Remark, err)
		}
		transport = t
	case model.ProxyModeOutbound:
		if node.OutboundTag == "" {
			return nil, common.NewError("outbound proxy mode requires an outbound tag")
		}
		port, err := (&NodeBridgeService{}).GetBridgePort(node.OutboundTag)
		if err != nil {
			return nil, common.NewErrorf("no local bridge for outbound %v: %v", node.OutboundTag, err)
		}
		t, err := socks5Transport(&url.URL{Scheme: "socks5", Host: fmt.Sprintf("127.0.0.1:%d", port)})
		if err != nil {
			return nil, err
		}
		transport = t
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   15 * time.Second,
	}

	base := strings.TrimRight(node.Address, "/")
	bp := strings.Trim(node.BasePath, "/")
	if bp != "" {
		base = base + "/" + bp
	}

	return &NodeClient{
		node:    node,
		client:  client,
		baseUrl: base,
	}, nil
}

func buildProxyURLTransport(rawProxyUrl string) (*http.Transport, error) {
	u, err := url.Parse(rawProxyUrl)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "https":
		return &http.Transport{Proxy: http.ProxyURL(u)}, nil
	case "socks5", "socks5h":
		return socks5Transport(u)
	default:
		return nil, common.NewErrorf("unsupported proxy scheme: %v", u.Scheme)
	}
}

func socks5Transport(u *url.URL) (*http.Transport, error) {
	var auth *proxy.Auth
	if u.User != nil {
		pw, _ := u.User.Password()
		auth = &proxy.Auth{User: u.User.Username(), Password: pw}
	}
	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
	if err != nil {
		return nil, err
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, common.NewError("socks5 dialer does not support context dialing")
	}
	return &http.Transport{DialContext: contextDialer.DialContext}, nil
}

// Login authenticates against the remote panel's /login endpoint, storing the
// session cookie in the client's cookie jar.
func (c *NodeClient) Login(ctx context.Context) error {
	form := url.Values{
		"username": {c.node.Username},
		"password": {c.node.Password},
	}
	msg, status, err := c.doRaw(ctx, http.MethodPost, "/login", form)
	if err != nil {
		return err
	}
	if status != http.StatusOK || msg == nil || !msg.Success {
		if msg != nil && msg.Msg != "" {
			return common.NewErrorf("login failed: %v", msg.Msg)
		}
		return common.NewErrorf("login failed with status %v", status)
	}
	c.loggedIn = true
	return nil
}

// doRaw performs a single HTTP request and decodes the body as entity.Msg.
// It does not attempt login/retry - that is handled by do().
func (c *NodeClient) doRaw(ctx context.Context, method, path string, form url.Values) (*entity.Msg, int, error) {
	reqUrl := c.baseUrl + path
	var bodyReader *strings.Reader
	if method == http.MethodGet {
		if form != nil && len(form) > 0 {
			reqUrl += "?" + form.Encode()
		}
		bodyReader = strings.NewReader("")
	} else {
		if form == nil {
			form = url.Values{}
		}
		bodyReader = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqUrl, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, resp.StatusCode, nil
	}

	var msg entity.Msg
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, resp.StatusCode, common.NewErrorf("invalid response from node (status %v): %v", resp.StatusCode, err)
	}
	return &msg, resp.StatusCode, nil
}

// do ensures the client is logged in, performs the request, and retries once
// after a fresh login if the session was rejected (401).
func (c *NodeClient) do(ctx context.Context, method, path string, form url.Values) (*entity.Msg, error) {
	if !c.loggedIn {
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
	}

	msg, status, err := c.doRaw(ctx, method, path, form)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		c.loggedIn = false
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		msg, status, err = c.doRaw(ctx, method, path, form)
		if err != nil {
			return nil, err
		}
		if status == http.StatusUnauthorized {
			return nil, common.NewError("unauthorized after re-login")
		}
	}

	if msg == nil {
		return nil, common.NewErrorf("empty response from node (status %v)", status)
	}
	if !msg.Success {
		return nil, common.NewError(msg.Msg)
	}
	return msg, nil
}

func remarshal(src interface{}, dst interface{}) error {
	if src == nil {
		return common.NewError("empty payload")
	}
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// GetInbounds returns all inbounds of the remote node.
func (c *NodeClient) GetInbounds(ctx context.Context) ([]model.Inbound, error) {
	msg, err := c.do(ctx, http.MethodGet, "/xui/API/inbounds/", nil)
	if err != nil {
		return nil, err
	}
	var inbounds []model.Inbound
	if err := remarshal(msg.Obj, &inbounds); err != nil {
		return nil, err
	}
	return inbounds, nil
}

// GetInbound returns a single inbound by id from the remote node.
func (c *NodeClient) GetInbound(ctx context.Context, id int) (*model.Inbound, error) {
	msg, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/xui/API/inbounds/get/%d", id), nil)
	if err != nil {
		return nil, err
	}
	var inbound model.Inbound
	if err := remarshal(msg.Obj, &inbound); err != nil {
		return nil, err
	}
	return &inbound, nil
}

// AddInbound creates an inbound on the remote node and returns the created inbound.
func (c *NodeClient) AddInbound(ctx context.Context, inbound *model.Inbound) (*model.Inbound, error) {
	form := url.Values{
		"remark":         {inbound.Remark},
		"enable":         {strconv.FormatBool(inbound.Enable)},
		"expiryTime":     {strconv.FormatInt(inbound.ExpiryTime, 10)},
		"listen":         {inbound.Listen},
		"port":           {strconv.Itoa(inbound.Port)},
		"protocol":       {string(inbound.Protocol)},
		"settings":       {inbound.Settings},
		"streamSettings": {inbound.StreamSettings},
		"sniffing":       {inbound.Sniffing},
	}
	msg, err := c.do(ctx, http.MethodPost, "/xui/API/inbounds/add", form)
	if err != nil {
		return nil, err
	}
	var result model.Inbound
	if err := remarshal(msg.Obj, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// AddClient adds a single client to an existing inbound on the remote node.
func (c *NodeClient) AddClient(ctx context.Context, inboundId int, client model.Client) error {
	settings, err := json.Marshal(map[string]interface{}{"clients": []model.Client{client}})
	if err != nil {
		return err
	}
	form := url.Values{
		"id":       {strconv.Itoa(inboundId)},
		"settings": {string(settings)},
	}
	_, err = c.do(ctx, http.MethodPost, "/xui/API/inbounds/addClient", form)
	return err
}

// UpdateClient updates a single client (identified by clientId, which is the
// UUID/password/email depending on protocol) on an inbound on the remote node.
func (c *NodeClient) UpdateClient(ctx context.Context, clientId string, inboundId int, client model.Client) error {
	settings, err := json.Marshal(map[string]interface{}{"clients": []model.Client{client}})
	if err != nil {
		return err
	}
	form := url.Values{
		"id":       {strconv.Itoa(inboundId)},
		"settings": {string(settings)},
	}
	_, err = c.do(ctx, http.MethodPost, "/xui/API/inbounds/updateClient/"+url.PathEscape(clientId), form)
	return err
}

// DelClient removes a client (by clientId = uuid/password/email) from an inbound on the remote node.
func (c *NodeClient) DelClient(ctx context.Context, inboundId int, clientId string) error {
	path := fmt.Sprintf("/xui/API/inbounds/%d/delClient/%s", inboundId, url.PathEscape(clientId))
	_, err := c.do(ctx, http.MethodPost, path, nil)
	return err
}

// ResetClientTraffic zeroes the traffic counters for a client (by email) on an inbound on the remote node.
func (c *NodeClient) ResetClientTraffic(ctx context.Context, inboundId int, email string) error {
	path := fmt.Sprintf("/xui/API/inbounds/%d/resetClientTraffic/%s", inboundId, url.PathEscape(email))
	_, err := c.do(ctx, http.MethodPost, path, nil)
	return err
}

// GetClientTraffic returns the traffic counters for the client with the given email.
func (c *NodeClient) GetClientTraffic(ctx context.Context, email string) (*xray.ClientTraffic, error) {
	msg, err := c.do(ctx, http.MethodGet, "/xui/API/inbounds/getClientTraffics/"+url.PathEscape(email), nil)
	if err != nil {
		return nil, err
	}
	if msg.Obj == nil {
		return nil, common.NewErrorf("no traffic data for %v", email)
	}
	var traffic xray.ClientTraffic
	if err := remarshal(msg.Obj, &traffic); err != nil {
		return nil, err
	}
	return &traffic, nil
}
