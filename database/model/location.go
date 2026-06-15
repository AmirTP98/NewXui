package model

// Location represents one routed location. Each location owns a local inbound
// (LocationInboundId); clients created on the designated master inbound are
// mirrored onto every location inbound (with a per-location email suffix), and
// their traffic is summed back onto the master client.
type Location struct {
	Id        int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Country   string `json:"country" form:"country"`   // ISO 2-letter code, e.g. "DE"
	Flag      string `json:"flag" form:"flag"`         // emoji flag, e.g. "🇩🇪"
	Remark    string `json:"remark" form:"remark"`     // label + email suffix
	InboundId int    `json:"inboundId" form:"inboundId"` // the local inbound for this location
	Enable    bool   `json:"enable" form:"enable"`
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
