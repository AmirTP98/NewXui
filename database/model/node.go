package model

const (
	ProxyModeNone     = "none"
	ProxyModeProxyUrl = "proxyUrl"
	ProxyModeOutbound = "outbound"
)

const (
	NodeStatusUnknown = "unknown"
	NodeStatusOnline  = "online"
	NodeStatusOffline = "offline"
)

type Node struct {
	Id       int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Remark   string `json:"remark" form:"remark"`
	Address  string `json:"address" form:"address"`
	BasePath string `json:"basePath" form:"basePath"`
	Username string `json:"username" form:"username"`
	Password string `json:"password" form:"password"`

	ProxyMode   string `json:"proxyMode" form:"proxyMode"`
	ProxyUrl    string `json:"proxyUrl" form:"proxyUrl"`
	OutboundTag string `json:"outboundTag" form:"outboundTag"`

	Enable bool `json:"enable" form:"enable"`

	// Port is the listen port of this node's own default inbound, built from the
	// shared universal template when the node is added (and its connection is OK).
	Port int `json:"port" form:"port"`
	// RemoteInboundId is the id of the default inbound created on this node
	// (0 = not created yet). Client add/edit operations target this inbound.
	RemoteInboundId int `json:"remoteInboundId" form:"remoteInboundId"`
	// InboundError holds the last error from creating this node's default inbound.
	InboundError string `json:"inboundError" form:"inboundError"`
	// LastSync is the last successful traffic-sync time for this node (unix ms).
	LastSync int64 `json:"lastSync" form:"lastSync"`

	Status    string `json:"status" form:"status"`
	LastCheck int64  `json:"lastCheck" form:"lastCheck"`
	LastError string `json:"lastError" form:"lastError"`
}

// NodeSharedInbound is the template used to create the same inbound on every node.
type NodeSharedInbound struct {
	Id             int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Remark         string `json:"remark" form:"remark"`
	Protocol       string `json:"protocol" form:"protocol"`
	Port           int    `json:"port" form:"port"`
	Settings       string `json:"settings" form:"settings"`
	StreamSettings string `json:"streamSettings" form:"streamSettings"`
	Sniffing       string `json:"sniffing" form:"sniffing"`
}

// NodeSharedInboundMap records which remote inbound id corresponds to the
// shared inbound on a given node.
type NodeSharedInboundMap struct {
	Id              int `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SharedInboundId int `json:"sharedInboundId" form:"sharedInboundId"`
	NodeId          int `json:"nodeId" form:"nodeId"`
	RemoteInboundId int `json:"remoteInboundId" form:"remoteInboundId"`
}

// NodeSharedClient is the single client tracked on the shared inbound across all nodes.
type NodeSharedClient struct {
	Id              int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	SharedInboundId int    `json:"sharedInboundId" form:"sharedInboundId"`
	ClientId        string `json:"clientId" form:"clientId"`
	Email           string `json:"email" form:"email"`
	TotalGB         int64  `json:"totalGB" form:"totalGB"`
	ExpiryTime      int64  `json:"expiryTime" form:"expiryTime"`
	Enable          bool   `json:"enable" form:"enable"`
}

// NodeClientTrafficSnapshot stores the last-seen traffic counters for a
// (node, email) pair, used to compute usage deltas between sync ticks.
type NodeClientTrafficSnapshot struct {
	Id     int    `json:"id" gorm:"primaryKey;autoIncrement"`
	NodeId int    `json:"nodeId" gorm:"uniqueIndex:idx_node_email"`
	Email  string `json:"email" gorm:"uniqueIndex:idx_node_email"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
}
