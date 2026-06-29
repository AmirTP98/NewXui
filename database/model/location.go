package model

// Location represents one routed location. Each location owns a local inbound
// (LocationInboundId); clients created on the designated master inbound are
// mirrored onto every location inbound (with a per-location email suffix), and
// their traffic is summed back onto the master client.
type Location struct {
	Id              int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Type            string `json:"type" form:"type" gorm:"default:'location'"` // "location" or "reality"
	Country         string `json:"country" form:"country"`
	Flag            string `json:"flag" form:"flag"`
	Remark          string `json:"remark" form:"remark"`
	Domain          string `json:"domain" form:"domain"`
	InboundId       int    `json:"inboundId" form:"inboundId"`
	MasterInboundId int    `json:"masterInboundId" form:"masterInboundId"` // reality: linked to one specific master
	Enable          bool   `json:"enable" form:"enable"`
}

// LocationTrafficSnapshot stores the last-seen traffic counters for a
// (location inbound, email) pair, used to compute usage deltas between ticks.
type LocationTrafficSnapshot struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundId int    `json:"inboundId" gorm:"uniqueIndex:idx_loc_inbound_email"`
	Email     string `json:"email" gorm:"uniqueIndex:idx_loc_inbound_email"`
	Up        int64  `json:"up"`
	Down      int64  `json:"down"`
}
