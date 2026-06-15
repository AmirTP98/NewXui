const ProxyModes = {
    NONE: 'none',
    PROXY_URL: 'proxyUrl',
    OUTBOUND: 'outbound',
};

const NodeStatus = {
    UNKNOWN: 'unknown',
    ONLINE: 'online',
    OFFLINE: 'offline',
};

class XNode {
    constructor(data) {
        this.id = 0;
        this.remark = '';
        this.address = '';
        this.basePath = '';
        this.username = '';
        this.password = '';
        this.proxyMode = ProxyModes.NONE;
        this.proxyUrl = '';
        this.outboundTag = '';
        this.enable = true;
        this.port = RandomUtil.randomIntRange(10000, 60000);
        this.remoteInboundId = 0;
        this.inboundError = '';
        this.lastSync = 0;
        this.status = NodeStatus.UNKNOWN;
        this.lastCheck = 0;
        this.lastError = '';

        if (data == null) {
            return;
        }
        ObjectUtil.cloneProps(this, data);
    }
}

class SharedClient {
    constructor(data) {
        this.id = RandomUtil.randomUUID();
        this.email = RandomUtil.randomLowerAndNum(8);
        this.totalGB = 0;
        this.expiryTime = 0;
        this.enable = true;

        if (data == null) {
            return;
        }
        if (data.clientId) this.id = data.clientId;
        if (data.id) this.id = data.id;
        if (data.email != null) this.email = data.email;
        if (data.totalGB != null) this.totalGB = data.totalGB;
        if (data.expiryTime != null) this.expiryTime = data.expiryTime;
        if (data.enable != null) this.enable = data.enable;
    }

    get _totalGB() {
        return toFixed(this.totalGB / ONE_GB, 2);
    }

    set _totalGB(gb) {
        this.totalGB = toFixed(gb * ONE_GB, 0);
    }

    get _expiryTime() {
        if (this.expiryTime === 0 || this.expiryTime === '') {
            return null;
        }
        return moment(this.expiryTime);
    }

    set _expiryTime(t) {
        this.expiryTime = (t == null || t === '') ? 0 : t.valueOf();
    }

    toClient() {
        return {
            id: this.id,
            password: this.id,
            flow: '',
            email: this.email,
            totalGB: this.totalGB,
            expiryTime: this.expiryTime,
            enable: this.enable,
            tgId: '',
            subId: RandomUtil.randomLowerAndNum(16),
            reset: 0,
        };
    }
}
