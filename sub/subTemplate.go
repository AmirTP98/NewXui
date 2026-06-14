package sub

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

func renderSubHTML(
	subId      string,
	subURL     string,
	remark     string,
	upBytes    int64,
	downBytes  int64,
	totalBytes int64,
	expireSec  int64,
	links      []string,
	isActive   bool,
) string {
	linksJSON, _ := json.Marshal(links)
	expireMs      := expireSec * 1000
	usedBytes     := upBytes + downBytes

	if remark == "" {
		remark = subId
	}

	activeInt := 0
	if isActive {
		activeInt = 1
	}

	dataScript := fmt.Sprintf(
		"<script>var __SUB={sId:%s,subUrl:%s,remark:%s,"+
			"upBytes:%d,downBytes:%d,totalBytes:%d,usedBytes:%d,expireMs:%d,links:%s,isActive:%d};</script>",
		jsonStr(subId), jsonStr(subURL), jsonStr(remark),
		upBytes, downBytes, totalBytes, usedBytes, expireMs,
		string(linksJSON), activeInt,
	)

	return subHTMLHead + dataScript + subHTMLBody
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func fmtBytesGo(b int64) string {
	if b <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i     := int(math.Log(float64(b)) / math.Log(1024))
	if i >= len(units) {
		i = len(units) - 1
	}
	return fmt.Sprintf("%.2f %s", float64(b)/math.Pow(1024, float64(i)), units[i])
}

func expireLabel(expireSec int64) string {
	if expireSec <= 0 {
		return "بدون تاریخ انقضا"
	}
	return time.Unix(expireSec, 0).In(time.FixedZone("IRST", 3*60*60+30*60)).Format("2006/01/02 15:04")
}

// ─────────────────────────────────────────────────────────────────────────────

const subHTMLHead = `<!DOCTYPE html>
<html lang="fa" dir="rtl">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>پنل سابسکریپشن</title>
<style>
  /* ── IranSansX local font ── */
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-thin.ttf') format('truetype');font-weight:100;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-ultralight.ttf') format('truetype');font-weight:200;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-light.ttf') format('truetype');font-weight:300;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-regular.ttf') format('truetype');font-weight:400;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-medium.ttf') format('truetype');font-weight:500;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-demibold.ttf') format('truetype');font-weight:600;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-bold.ttf') format('truetype');font-weight:700;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-extrabold.ttf') format('truetype');font-weight:800;font-style:normal;font-display:swap}
  @font-face{font-family:'IranSansX';src:url('../assets/iransansx-heavy.ttf') format('truetype');font-weight:900;font-style:normal;font-display:swap}

  :root {
    --font-sans: 'IranSansX', system-ui, -apple-system, BlinkMacSystemFont,
                 'Segoe UI', Tahoma, Arial, sans-serif;
    --font-mono: ui-monospace, 'Cascadia Code', Consolas,
                 'Courier New', monospace;

    color-scheme: dark;
    --bg: #050a10; --bg2: #0a1020;
    --glass: rgba(255,255,255,0.045); --glass2: rgba(255,255,255,0.07);
    --glass-border: rgba(255,255,255,0.12); --glass-shine: rgba(255,255,255,0.18);
    --text: #f0f4ff; --muted: #7b8ba8;
    --accent: #60a5fa; --accent2: #a78bfa;
    --neon: #22d3ee; --neon2: #818cf8;
    --neon-glow: rgba(34,211,238,0.4); --neon-glow2: rgba(129,140,248,0.35);
    --danger: #f87171; --success: #34d399;
    --bar-bg: rgba(255,255,255,0.06);
    --shadow: 0 8px 32px rgba(0,0,0,0.5);
    --radius: 24px; --grid-color: rgba(255,255,255,0.03);
  }
  body.light {
    color-scheme: light;
    --bg: #dfe6f0; --bg2: #d0daea;
    --glass: rgba(255,255,255,0.5); --glass2: rgba(255,255,255,0.6);
    --glass-border: rgba(255,255,255,0.7); --glass-shine: rgba(255,255,255,1);
    --text: #0f172a; --muted: #64748b;
    --accent: #3b82f6; --accent2: #7c3aed;
    --neon: #06b6d4; --neon2: #6366f1;
    --neon-glow: rgba(6,182,212,0.3); --neon-glow2: rgba(99,102,241,0.25);
    --bar-bg: rgba(0,0,0,0.07);
    --shadow: 0 8px 32px rgba(0,0,0,0.08);
    --grid-color: rgba(0,0,0,0.045);
  }
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:var(--font-sans);background:var(--bg);color:var(--text);min-height:100vh;overflow-x:hidden}

  /* Background */
  .bg-scene{position:fixed;inset:0;z-index:0;pointer-events:none;overflow:hidden}
  .bg-grid{position:absolute;inset:0;
    background-image:linear-gradient(var(--grid-color) 1px,transparent 1px),
                     linear-gradient(90deg,var(--grid-color) 1px,transparent 1px);
    background-size:56px 56px;
    mask-image:radial-gradient(ellipse 80% 60% at 50% 35%,black 20%,transparent 100%);
    -webkit-mask-image:radial-gradient(ellipse 80% 60% at 50% 35%,black 20%,transparent 100%)}
  #particles-canvas{position:fixed;inset:0;z-index:0;pointer-events:none}
  .blob{position:absolute;border-radius:50%}
  .blob-1{width:600px;height:600px;background:radial-gradient(circle,rgba(96,165,250,.5),rgba(96,165,250,.08) 65%,transparent 80%);top:-14%;left:-10%;filter:blur(70px);animation:bf 9s ease-in-out infinite alternate}
  .blob-2{width:520px;height:520px;background:radial-gradient(circle,rgba(167,139,250,.45),rgba(167,139,250,.06) 65%,transparent 80%);top:22%;right:-12%;filter:blur(70px);animation:bf 11s ease-in-out infinite alternate-reverse}
  .blob-3{width:460px;height:460px;background:radial-gradient(circle,rgba(34,211,238,.42),rgba(34,211,238,.06) 65%,transparent 80%);bottom:-10%;left:12%;filter:blur(70px);animation:bf 10s ease-in-out infinite alternate;animation-delay:-3s}
  body.light .blob{opacity:.4}
  @keyframes bf{0%{transform:translate(0,0) scale(1)}33%{transform:translate(55px,-45px) scale(1.12)}66%{transform:translate(-35px,60px) scale(.92)}100%{transform:translate(45px,25px) scale(1.06)}}

  /* Layout */
  .wrap{position:relative;z-index:1;width:min(960px,calc(100% - 32px));margin:28px auto 40px;display:grid;gap:16px}

  /* Glass card */
  .glass{background:var(--glass);border:1px solid var(--glass-border);border-radius:var(--radius);
    padding:24px;box-shadow:var(--shadow),inset 0 1px 0 rgba(255,255,255,.08);
    backdrop-filter:blur(28px) saturate(1.6);-webkit-backdrop-filter:blur(28px) saturate(1.6);
    position:relative;overflow:hidden;animation:fadeUp .6s ease both}
  .glass:nth-child(2){animation-delay:.1s}.glass:nth-child(3){animation-delay:.2s}
  .glass:nth-child(4){animation-delay:.3s}.glass:nth-child(5){animation-delay:.4s}
  .glass::before{content:'';position:absolute;top:0;left:0;right:0;height:1px;
    background:linear-gradient(90deg,transparent 5%,var(--glass-shine) 50%,transparent 95%);opacity:.7}
  .glass .spotlight{position:absolute;inset:0;pointer-events:none;z-index:50;opacity:0;
    transition:opacity .4s;border-radius:var(--radius)}
  .glass:hover .spotlight{opacity:1}
  @keyframes fadeUp{from{opacity:0;transform:translateY(24px)}to{opacity:1;transform:translateY(0)}}

  /* Hero */
  .topbar{display:flex;align-items:center;justify-content:space-between;gap:12px;position:relative;z-index:2}
  .brand{font-size:13px;font-weight:500;color:var(--muted);letter-spacing:.04em}
  .hero h1{position:relative;z-index:2;margin:12px 0 8px;font-size:clamp(28px,5vw,44px);font-weight:800;
    background:linear-gradient(135deg,var(--accent),var(--accent2),var(--neon));
    -webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text}
  .sub-id{position:relative;z-index:2;color:var(--muted);font-size:13px;word-break:break-all;
    font-family:var(--font-mono);opacity:.8;direction:ltr;text-align:left}

  /* Theme toggle */
  .theme-toggle{display:flex;align-items:center;gap:8px;padding:8px 14px;border-radius:999px;
    border:1px solid var(--glass-border);background:var(--glass);backdrop-filter:blur(12px);
    cursor:pointer;user-select:none;transition:all .3s}
  .theme-toggle:hover{border-color:var(--accent);box-shadow:0 0 16px var(--neon-glow)}
  .theme-icon{font-size:15px;line-height:1}
  .theme-switch{position:relative;width:38px;height:20px;border-radius:999px;
    background:rgba(148,163,184,.3);transition:background .3s}
  .theme-switch::after{content:'';position:absolute;top:2px;left:2px;width:16px;height:16px;
    border-radius:50%;background:#fff;box-shadow:0 1px 4px rgba(0,0,0,.3);
    transition:transform .3s cubic-bezier(.68,-.55,.27,1.55)}
  body:not(.light) .theme-switch{background:linear-gradient(135deg,var(--accent),var(--neon))}
  body:not(.light) .theme-switch::after{transform:translateX(18px)}

  /* Stats */
  .stats-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));gap:12px;position:relative;z-index:2}
  .stat-card{background:var(--glass2);border:1px solid var(--glass-border);border-radius:20px;
    padding:18px;position:relative;overflow:hidden;transition:transform .3s,box-shadow .3s}
  .stat-card:hover{transform:translateY(-2px);box-shadow:0 12px 32px rgba(0,0,0,.3)}
  .stat-card::before{content:'';position:absolute;top:0;left:0;right:0;height:1px;
    background:linear-gradient(90deg,transparent,var(--glass-shine),transparent)}
  .stat-label{display:block;font-size:11px;font-weight:600;letter-spacing:.02em;
    color:var(--muted);margin-bottom:10px}
  .stat-value{display:block;font-size:22px;font-weight:700;height:32px;overflow:hidden}

  /* Wheel */
  .wheel-container{position:relative;height:32px;overflow:hidden}
  .wheel-track{position:absolute;top:0;left:0;animation:wheelSpin 1.2s cubic-bezier(.23,1,.32,1) forwards}
  .wheel-item{height:32px;display:flex;align-items:center;font-size:22px;font-weight:700;color:var(--muted);opacity:.3}
  .wheel-item.final{color:var(--text);opacity:1}
  @keyframes wheelSpin{0%{transform:translateY(0)}100%{transform:var(--wheel-end)}}

  /* Usage bar */
  .usage-section{position:relative;z-index:2;display:grid;gap:16px}
  .usage-header{display:flex;justify-content:space-between;align-items:baseline}
  .usage-title{font-size:15px;font-weight:700}
  .usage-pct{font-size:28px;font-weight:800;
    background:linear-gradient(135deg,var(--accent),var(--neon));
    -webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text}
  .usage-bar-outer{width:100%;height:18px;border-radius:999px;background:var(--bar-bg);
    overflow:hidden;border:1px solid rgba(255,255,255,.06);position:relative;
    cursor:pointer;transition:transform .3s,box-shadow .3s}
  .usage-bar-outer:hover{transform:scaleY(1.35);
    box-shadow:0 0 30px var(--neon-glow),0 0 60px var(--neon-glow2)}
  .usage-bar-inner{height:100%;border-radius:999px;
    background:linear-gradient(90deg,#3b82f6,#8b5cf6,#22d3ee,#60a5fa);
    background-size:300% 100%;width:0;position:relative;
    animation:barFill 2s cubic-bezier(.22,1,.36,1) forwards;
    box-shadow:0 0 12px var(--neon-glow),0 0 24px var(--neon-glow2);overflow:hidden}
  .usage-bar-inner::before{content:'';position:absolute;inset:0;
    background:linear-gradient(90deg,transparent,rgba(255,255,255,.25),transparent);
    background-size:60% 100%;animation:barWave 2.5s ease-in-out infinite}
  @keyframes barFill{0%{width:0;background-position:0}100%{width:var(--bar-pct);background-position:100%}}
  @keyframes barWave{0%{background-position:-100% 0}100%{background-position:200% 0}}
  .usage-labels{display:flex;justify-content:space-between;font-size:14px;font-weight:600;color:var(--muted)}

  /* Timer ring */
  .timer-section{display:flex;flex-direction:column;align-items:center;gap:12px;position:relative;z-index:2}
  .timer-ring-wrap{position:relative;width:170px;height:170px}
  .timer-ring-bg,.timer-ring-fg{position:absolute;inset:0}
  .timer-ring-bg circle{fill:none;stroke:var(--bar-bg);stroke-width:8}
  .timer-ring-fg circle{fill:none;stroke:url(#timerGrad);stroke-width:8;stroke-linecap:round;
    stroke-dasharray:var(--circ);stroke-dashoffset:var(--circ);
    animation:ringFill 2s cubic-bezier(.34,1.56,.64,1) forwards;
    transform:rotate(-90deg);transform-origin:center;
    filter:drop-shadow(0 0 10px var(--neon-glow))}
  @keyframes ringFill{to{stroke-dashoffset:var(--ring-offset)}}
  .timer-center{position:absolute;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center}
  .timer-value{font-size:28px;font-weight:800}
  .timer-sub-label{font-size:11px;font-weight:600;color:var(--muted);letter-spacing:.04em;margin-top:2px}
  .expire-date{font-size:13px;color:var(--muted);font-weight:600;text-align:center;font-family:var(--font-mono);direction:ltr}

  /* Infinity */
  .infinity-wrap{display:flex;flex-direction:column;align-items:center;gap:12px;position:relative;z-index:2}
  .infinity-svg{width:170px;height:100px}
  .infinity-path-bg{fill:none;stroke:var(--bar-bg);stroke-width:4;stroke-linecap:round}
  .infinity-path-glow{fill:none;stroke:url(#infGrad);stroke-width:4.5;stroke-linecap:round;
    stroke-dasharray:60 300;animation:infTrace 2.8s linear infinite;
    filter:drop-shadow(0 0 12px var(--neon-glow)) drop-shadow(0 0 24px var(--neon-glow2))}
  @keyframes infTrace{0%{stroke-dashoffset:0}100%{stroke-dashoffset:-360}}
  .infinity-label{font-size:15px;font-weight:700;color:var(--muted);letter-spacing:.04em}

  .meters{display:grid;grid-template-columns:1fr 1fr;gap:16px}
  @media(max-width:640px){.meters{grid-template-columns:1fr}}

  /* Sub link section */
  .section-title{font-size:13px;font-weight:600;letter-spacing:.04em;
    color:var(--muted);margin-bottom:14px;display:flex;align-items:center;gap:8px}
  .section-title::after{content:'';flex:1;height:1px;background:var(--glass-border)}

  .sub-url-row{display:flex;gap:10px;align-items:stretch}
  .sub-url-text{flex:1;background:rgba(0,0,0,.3);color:var(--text);border:1px solid var(--glass-border);
    border-radius:14px;padding:12px 16px;font:12px/1.6 var(--font-mono);
    word-break:break-all;white-space:pre-wrap;cursor:pointer;transition:border-color .2s;
    direction:ltr;text-align:left}
  .sub-url-text:hover{border-color:var(--accent)}
  body.light .sub-url-text{background:rgba(255,255,255,.6)}

  .btn-neon{position:relative;border:none;border-radius:14px;padding:12px 22px;
    font-size:13px;font-weight:700;font-family:var(--font-sans);cursor:pointer;color:#fff;
    background:linear-gradient(135deg,rgba(96,165,250,.18),rgba(34,211,238,.18));
    border:1px solid rgba(96,165,250,.3);backdrop-filter:blur(16px);overflow:hidden;
    transition:all .3s;white-space:nowrap;flex-shrink:0}
  .btn-neon:hover{transform:translateY(-1px);box-shadow:0 0 28px var(--neon-glow),0 0 56px var(--neon-glow2)}
  .btn-neon span{position:relative;z-index:2}

  /* Connection link cards */
  .link-card{background:var(--glass2);border:1px solid var(--glass-border);border-radius:18px;
    padding:16px 18px;display:flex;align-items:center;gap:14px;
    cursor:pointer;transition:border-color .25s,transform .2s,box-shadow .2s;
    position:relative;overflow:hidden;direction:ltr}
  .link-card::before{content:'';position:absolute;top:0;left:0;right:0;height:1px;
    background:linear-gradient(90deg,transparent,var(--glass-shine),transparent)}
  .link-card:hover{border-color:var(--accent);transform:translateY(-1px);
    box-shadow:0 8px 24px rgba(0,0,0,.3),0 0 20px var(--neon-glow)}
  .link-card:active{transform:translateY(0)}
  .link-proto-badge{font-size:10px;font-weight:700;letter-spacing:.08em;padding:4px 10px;
    border-radius:8px;background:rgba(96,165,250,.15);border:1px solid rgba(96,165,250,.25);
    color:var(--accent);text-transform:uppercase;white-space:nowrap;flex-shrink:0}
  .link-card-body{flex:1;min-width:0}
  .link-name{font-size:14px;font-weight:600;color:var(--text);
    white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-bottom:4px}
  .link-uri-short{font-size:11px;color:var(--muted);font-family:var(--font-mono);
    white-space:nowrap;overflow:hidden;text-overflow:ellipsis;direction:ltr;text-align:left}
  .link-copy-icon{font-size:16px;opacity:.5;transition:opacity .2s;flex-shrink:0}
  .link-card:hover .link-copy-icon{opacity:1}
  .links-empty{text-align:center;color:var(--muted);padding:32px 16px;font-size:14px}

  /* Platform / App download section */
  .platform-tabs{display:flex;gap:8px;margin-bottom:20px;flex-wrap:wrap}
  .platform-tab{padding:8px 20px;border-radius:999px;border:1px solid var(--glass-border);
    background:var(--glass);color:var(--muted);font-size:13px;font-weight:600;
    cursor:pointer;transition:all .25s;font-family:var(--font-sans)}
  .platform-tab.active,.platform-tab:hover{border-color:var(--accent);color:var(--accent);
    background:rgba(96,165,250,.12);box-shadow:0 0 16px var(--neon-glow)}
  .platform-panel{display:none}
  .platform-panel.active{display:block;animation:fadeUp .3s ease both}
  .app-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:10px}
  .app-btn{display:flex;align-items:center;gap:12px;padding:14px 16px;
    background:var(--glass2);border:1px solid var(--glass-border);border-radius:16px;
    text-decoration:none;color:var(--text);transition:all .25s;
    position:relative;overflow:hidden;direction:ltr}
  .app-btn::before{content:'';position:absolute;top:0;left:0;right:0;height:1px;
    background:linear-gradient(90deg,transparent,var(--glass-shine),transparent)}
  .app-btn:hover{border-color:var(--accent);transform:translateY(-2px);
    box-shadow:0 8px 24px rgba(0,0,0,.3),0 0 20px var(--neon-glow)}
  .app-icon-wrap{width:38px;height:38px;border-radius:10px;display:flex;align-items:center;
    justify-content:center;font-size:20px;background:rgba(96,165,250,.1);
    border:1px solid rgba(96,165,250,.2);flex-shrink:0}
  .app-info{display:flex;flex-direction:column;gap:3px;min-width:0;flex:1}
  .app-name{font-size:14px;font-weight:700;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .app-source{font-size:11px;color:var(--muted);white-space:nowrap}

  /* Toast */
  .toast-overlay{position:fixed;inset:0;z-index:9998;background:rgba(0,0,0,0);
    backdrop-filter:blur(0px);transition:background .4s,backdrop-filter .4s;
    display:flex;align-items:center;justify-content:center;pointer-events:none;opacity:0}
  .toast-overlay.active{pointer-events:auto;opacity:1;background:rgba(0,0,0,.35);backdrop-filter:blur(8px)}
  .toast-card{background:var(--glass2);border:1px solid var(--glass-border);border-radius:28px;
    padding:36px 48px;box-shadow:0 0 60px var(--neon-glow),0 0 120px var(--neon-glow2),var(--shadow);
    backdrop-filter:blur(32px) saturate(1.8);text-align:center;
    transform:scale(.5) translateY(40px);opacity:0;
    transition:transform .5s cubic-bezier(.34,1.56,.64,1),opacity .4s;position:relative;overflow:hidden}
  .toast-card::before{content:'';position:absolute;top:0;left:0;right:0;height:1px;
    background:linear-gradient(90deg,transparent,var(--glass-shine),transparent)}
  .toast-overlay.active .toast-card{transform:scale(1) translateY(0);opacity:1}
  .toast-icon{width:64px;height:64px;margin:0 auto 16px;border-radius:50%;
    background:linear-gradient(135deg,rgba(34,211,238,.15),rgba(96,165,250,.15));
    border:2px solid rgba(34,211,238,.3);display:flex;align-items:center;justify-content:center;
    font-size:30px;animation:tp 1.5s ease-in-out infinite}
  @keyframes tp{0%,100%{box-shadow:0 0 20px var(--neon-glow)}50%{box-shadow:0 0 40px var(--neon-glow),0 0 60px var(--neon-glow2)}}
  .toast-title{font-size:20px;font-weight:800;margin-bottom:6px;
    background:linear-gradient(135deg,var(--accent),var(--neon));
    -webkit-background-clip:text;-webkit-text-fill-color:transparent;background-clip:text}
  .toast-msg{font-size:13px;color:var(--muted)}
  .toast-card.fail .toast-icon{border-color:rgba(248,113,113,.4);background:rgba(248,113,113,.12)}
  .toast-card.fail .toast-title{background:linear-gradient(135deg,var(--danger),#fbbf24);-webkit-background-clip:text;background-clip:text}
  body.toast-active .wrap{filter:blur(4px) brightness(.6);transition:filter .4s;pointer-events:none}
  body:not(.toast-active) .wrap{filter:none;transition:filter .4s}

  .footer{text-align:center;padding:8px;font-size:11px;color:var(--muted);opacity:.5;position:relative;z-index:2}

  @media(max-width:640px){
    .topbar{flex-direction:column;align-items:flex-start}
    .stats-grid{grid-template-columns:repeat(2,1fr)}
    .sub-url-row{flex-direction:column}
    .btn-neon{width:100%;text-align:center}
    .toast-card{padding:28px 24px}
    .app-grid{grid-template-columns:repeat(2,1fr)}
  }
</style>
</head>
<body>
<div class="bg-scene">
  <div class="bg-grid"></div>
  <div class="blob blob-1"></div>
  <div class="blob blob-2"></div>
  <div class="blob blob-3"></div>
</div>
<canvas id="particles-canvas"></canvas>

<svg style="position:absolute;width:0;height:0;">
  <defs>
    <linearGradient id="timerGrad" x1="0%" y1="0%" x2="100%" y2="100%">
      <stop offset="0%" stop-color="#60a5fa"/>
      <stop offset="50%" stop-color="#22d3ee"/>
      <stop offset="100%" stop-color="#a78bfa"/>
    </linearGradient>
    <linearGradient id="infGrad" x1="0%" y1="0%" x2="100%" y2="0%">
      <stop offset="0%" stop-color="#22d3ee"/>
      <stop offset="50%" stop-color="#a78bfa"/>
      <stop offset="100%" stop-color="#22d3ee"/>
    </linearGradient>
  </defs>
</svg>

<div class="toast-overlay" id="toast-overlay">
  <div class="toast-card" id="toast-card">
    <div class="toast-icon" id="toast-icon">&#x2713;</div>
    <div class="toast-title" id="toast-title">کپی شد!</div>
    <div class="toast-msg" id="toast-msg"></div>
  </div>
</div>

<main class="wrap">
  <!-- Hero -->
  <section class="glass hero">
    <div class="spotlight"></div>
    <div class="topbar">
      <div class="brand">&#x2B21; پنل سابسکریپشن</div>
      <button class="theme-toggle" type="button" id="theme-toggle" aria-label="تغییر تم">
        <span class="theme-icon">&#x2600;&#xFE0F;</span>
        <span class="theme-switch" aria-hidden="true"></span>
        <span class="theme-icon">&#x1F319;</span>
      </button>
    </div>
    <h1 id="hero-title">سابسکریپشن</h1>
    <div class="sub-id" id="sub-id-display"></div>
  </section>

  <!-- Stats -->
  <section class="glass">
    <div class="spotlight"></div>
    <div class="stats-grid" id="stats-grid"></div>
  </section>

  <!-- Usage + Timer -->
  <section class="glass">
    <div class="spotlight"></div>
    <div class="meters">
      <div class="usage-section">
        <div class="usage-header">
          <span class="usage-title">مصرف داده</span>
          <span class="usage-pct" id="usage-pct">0%</span>
        </div>
        <div class="usage-bar-outer">
          <div class="usage-bar-inner" id="usage-bar" style="--bar-pct:0%"></div>
        </div>
        <div class="usage-labels">
          <span id="used-label">&#x2014;</span>
          <span id="total-label">&#x2014;</span>
        </div>
      </div>
      <div id="time-display"></div>
    </div>
  </section>

  <!-- Subscription URL -->
  <section class="glass">
    <div class="spotlight"></div>
    <div class="section-title">&#x1F4CB; لینک سابسکریپشن</div>
    <div class="sub-url-row">
      <div class="sub-url-text" id="sub-url-text" title="برای کپی کلیک کنید"></div>
      <button class="btn-neon" id="copy-sub-btn" type="button"><span>کپی</span></button>
    </div>
  </section>

  <!-- Connection Links -->
  <section class="glass">
    <div class="spotlight"></div>
    <div class="section-title" id="links-title">&#x1F517; لینک‌های اتصال</div>
    <div id="links-container"></div>
  </section>

  <!-- App Downloads -->
  <section class="glass">
    <div class="spotlight"></div>
    <div class="section-title">&#x1F4F1; دانلود اپلیکیشن</div>
    <div class="platform-tabs">
      <button class="platform-tab active" type="button" data-target="tab-android">&#x1F916; اندروید</button>
      <button class="platform-tab" type="button" data-target="tab-ios">&#x1F34E; iOS</button>
      <button class="platform-tab" type="button" data-target="tab-windows">&#x1F5A5; ویندوز</button>
    </div>
    <!-- Android -->
    <div id="tab-android" class="platform-panel active">
      <div class="app-grid">
        <a class="app-btn" href="https://play.google.com/store/apps/details?id=com.v2ray.ang" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x25B6;</div>
          <div class="app-info"><div class="app-name">V2RayNG</div><div class="app-source">Google Play</div></div>
        </a>
        <a class="app-btn" href="https://s31.uupload.ir/files/eayansct/image/v2rayNG_2.0.15_universal.apk" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x2B07;</div>
          <div class="app-info"><div class="app-name">V2RayNG</div><div class="app-source">دانلود APK</div></div>
        </a>
        <a class="app-btn" href="https://play.google.com/store/apps/details?id=app.hiddify.com" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x25B6;</div>
          <div class="app-info"><div class="app-name">Hiddify</div><div class="app-source">Google Play</div></div>
        </a>
        <a class="app-btn" href="https://github.com/hiddify/hiddify-app/releases/latest" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x2B07;</div>
          <div class="app-info"><div class="app-name">Hiddify</div><div class="app-source">دانلود APK</div></div>
        </a>
      </div>
    </div>
    <!-- iOS -->
    <div id="tab-ios" class="platform-panel">
      <div class="app-grid">
        <a class="app-btn" href="https://apps.apple.com/app/shadowrocket/id932747118" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x1F680;</div>
          <div class="app-info"><div class="app-name">Shadowrocket</div><div class="app-source">App Store</div></div>
        </a>
        <a class="app-btn" href="https://apps.apple.com/app/streisand/id6450534064" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x1F512;</div>
          <div class="app-info"><div class="app-name">Streisand</div><div class="app-source">App Store</div></div>
        </a>
        <a class="app-btn" href="https://apps.apple.com/app/v2box-v2ray-client/id6446814690" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x1F4E6;</div>
          <div class="app-info"><div class="app-name">V2Box</div><div class="app-source">App Store</div></div>
        </a>
        <a class="app-btn" href="https://apps.apple.com/app/hiddify-proxy-vpn/id6596777532" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x1F6E1;</div>
          <div class="app-info"><div class="app-name">Hiddify</div><div class="app-source">App Store</div></div>
        </a>
      </div>
    </div>
    <!-- Windows -->
    <div id="tab-windows" class="platform-panel">
      <div class="app-grid">
        <a class="app-btn" href="https://s31.uupload.ir/files/eayansct/image/Hiddify-Windows-Setup-x64.exe" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x1F5A5;</div>
          <div class="app-info"><div class="app-name">Hiddify</div><div class="app-source">دانلود نصب‌کننده</div></div>
        </a>
        <a class="app-btn" href="https://s31.uupload.ir/files/eayansct/image/nekoray-4.0.1-2024-12-12-windows64_5.zip" target="_blank" rel="noopener noreferrer">
          <div class="app-icon-wrap">&#x1F431;</div>
          <div class="app-info"><div class="app-name">NekoRay</div><div class="app-source">دانلود ZIP</div></div>
        </a>
      </div>
    </div>
  </section>
</main>
<div class="footer">پنل سابسکریپشن</div>
`

const subHTMLBody = `
<script>
(function(){
  var D        = window.__SUB || {};
  var subUrl   = D.subUrl   || '';
  var remark   = D.remark   || D.sId || '';
  var up       = D.upBytes    || 0;
  var down     = D.downBytes  || 0;
  var total    = D.totalBytes || 0;
  var used     = D.usedBytes  || 0;
  var expMs    = D.expireMs   || 0;
  var links    = D.links      || [];
  var isActive = D.isActive === 1;

  // ── Helpers ──────────────────────────────────────────────────────────
  function fmt(b) {
    if (b <= 0) return '0 B';
    var u = ['B','KB','MB','GB','TB'];
    var i = Math.min(Math.floor(Math.log(b) / Math.log(1024)), 4);
    return (b / Math.pow(1024, i)).toFixed(2) + ' ' + u[i];
  }
  function esc(s) {
    return String(s)
      .replace(/&/g,'&amp;').replace(/</g,'&lt;')
      .replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }
  function proto(link) {
    var m = link.match(/^([a-z]+):\/\//i);
    return m ? m[1].toUpperCase() : '?';
  }
  function linkName(link, idx) {
    try {
      if (link.indexOf('vmess://') === 0) {
        var b64 = link.replace('vmess://','').split('#')[0];
        var j = JSON.parse(atob(b64));
        if (j && j.ps) return j.ps;
      }
      var h = link.lastIndexOf('#');
      if (h !== -1) {
        var n = decodeURIComponent(link.substring(h + 1));
        if (n.trim()) return n.trim();
      }
    } catch(e) {}
    return 'Link ' + (idx + 1);
  }
  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      return navigator.clipboard.writeText(text);
    }
    return new Promise(function(res, rej) {
      var a = document.createElement('textarea');
      a.value = text; a.setAttribute('readonly','');
      a.style.cssText = 'position:fixed;opacity:0';
      document.body.appendChild(a); a.select(); a.setSelectionRange(0, a.value.length);
      document.execCommand('copy') ? res() : rej();
      document.body.removeChild(a);
    });
  }
  function showToast(label, success) {
    var overlay = document.getElementById('toast-overlay');
    var card = document.getElementById('toast-card');
    var icon = document.getElementById('toast-icon');
    var title = document.getElementById('toast-title');
    var msg = document.getElementById('toast-msg');
    if (success) {
      card.className = 'toast-card'; icon.textContent = '✓';
      title.textContent = 'کپی شد！'; msg.textContent = label;
    } else {
      card.className = 'toast-card fail'; icon.textContent = '✗';
      title.textContent = 'خطا！'; msg.textContent = 'به صورت دستی کپی کنید';
    }
    document.body.classList.add('toast-active');
    overlay.classList.add('active');
    setTimeout(function() {
      overlay.classList.remove('active');
      document.body.classList.remove('toast-active');
    }, 1600);
  }
  document.getElementById('toast-overlay').addEventListener('click', function(e) {
    if (e.target === this) { this.classList.remove('active'); document.body.classList.remove('toast-active'); }
  });

  // ── Hero ──────────────────────────────────────────────────────────────
  document.title = remark || 'پنل سابسکریپشن';
  document.getElementById('hero-title').textContent = remark || 'سابسکریپشن';
  document.getElementById('sub-id-display').textContent = D.sId || '';

  // ── Stats ─────────────────────────────────────────────────────────────
  var statDefs = [
    { label:'مصرف شده',   val: fmt(used) },
    { label:'باقی‌مانده', val: total > 0 ? fmt(Math.max(0, total - used)) : '∞' },
    { label:'آپلود',      val: fmt(up)   },
    { label:'دانلود',     val: fmt(down) },
    { label:'حجم کل',    val: total > 0 ? fmt(total) : 'نامحدود' },
  ];
  var grid = document.getElementById('stats-grid');
  statDefs.forEach(function(s) {
    var card = document.createElement('div');
    card.className = 'stat-card';
    card.innerHTML =
      '<span class="stat-label">' + esc(s.label) + '</span>' +
      '<div class="stat-value"><div class="wheel-container" data-final="' + esc(s.val) + '"></div></div>';
    grid.appendChild(card);
  });
  // Wheel spin
  document.querySelectorAll('.wheel-container').forEach(function(el) {
    var finalText = el.getAttribute('data-final');
    var track = document.createElement('div');
    track.className = 'wheel-track';
    var dummies = genDummies(finalText, 8);
    dummies.forEach(function(t) {
      var d = document.createElement('div'); d.className = 'wheel-item'; d.textContent = t; track.appendChild(d);
    });
    var fin = document.createElement('div'); fin.className = 'wheel-item final'; fin.textContent = finalText; track.appendChild(fin);
    track.style.setProperty('--wheel-end', 'translateY(-' + (dummies.length * 32) + 'px)');
    el.appendChild(track);
  });
  function genDummies(text, n) {
    var m = text.match(/^([\d.]+)\s*(.*)/), r = [];
    if (m) {
      for (var i = 0; i < n; i++)
        r.push((Math.random() * parseFloat(m[1]) * 2).toFixed(2) + (m[2] ? ' ' + m[2] : ''));
    } else {
      var c = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
      for (var j = 0; j < n; j++) {
        var s = '';
        for (var k = 0; k < Math.min(text.length, 8); k++) s += c[Math.floor(Math.random() * c.length)];
        r.push(s);
      }
    }
    return r;
  }

  // ── Usage Bar ─────────────────────────────────────────────────────────
  var pct = total > 0 ? Math.min(100, Math.max(0, used / total * 100)) : 0;
  document.getElementById('usage-bar').style.setProperty('--bar-pct', pct.toFixed(1) + '%');
  document.getElementById('used-label').textContent = fmt(used) + ' مصرف شده';
  document.getElementById('total-label').textContent = total > 0 ? fmt(total) : 'نامحدود';
  (function animNum(el, from, to, dur, fmtFn) {
    var st = performance.now();
    (function tick(now) {
      var t = Math.min(1, (now - st) / dur);
      el.textContent = fmtFn(from + (to - from) * (1 - Math.pow(1 - t, 3)));
      if (t < 1) requestAnimationFrame(tick);
    })(performance.now());
  })(document.getElementById('usage-pct'), 0, pct, 1800, function(v) { return v.toFixed(1) + '%'; });

  // ── Timer Ring ────────────────────────────────────────────────────────
  var td = document.getElementById('time-display');
  if (expMs <= 0) {
    td.innerHTML =
      '<div class="infinity-wrap">' +
        '<svg class="infinity-svg" viewBox="0 0 160 90">' +
          '<path class="infinity-path-bg" d="M40,45 C40,20 10,20 10,45 C10,70 40,70 40,45 C40,20 70,20 70,45 C70,70 40,70 40,45" transform="translate(40,0)"/>' +
          '<path class="infinity-path-glow" d="M40,45 C40,20 10,20 10,45 C10,70 40,70 40,45 C40,20 70,20 70,45 C70,70 40,70 40,45" transform="translate(40,0)"/>' +
        '</svg>' +
        '<span class="infinity-label">بدون محدودیت زمانی</span>' +
      '</div>';
  } else {
    var expDate  = new Date(expMs);
    var daysLeft = Math.max(0, Math.ceil((expDate - new Date()) / 86400000));
    var periods  = [30, 60, 90, 120, 180, 365, 730];
    var totP = 30;
    for (var pi = 0; pi < periods.length; pi++) { if (daysLeft <= periods[pi]) { totP = periods[pi]; break; } }
    var ringPct = Math.min(100, daysLeft / totP * 100);
    var r = 68, circ = 2 * Math.PI * r, offset = circ * (1 - ringPct / 100);
    var expStr = expDate.toISOString().replace('T',' ').substring(0,16);
    td.innerHTML =
      '<div class="timer-section">' +
        '<div class="timer-ring-wrap">' +
          '<svg class="timer-ring-bg" viewBox="0 0 170 170"><circle cx="85" cy="85" r="' + r + '"/></svg>' +
          '<svg class="timer-ring-fg" viewBox="0 0 170 170" style="--circ:' + circ.toFixed(2) + ';--ring-offset:' + offset.toFixed(2) + ';"><circle cx="85" cy="85" r="' + r + '"/></svg>' +
          '<div class="timer-center">' +
            '<span class="timer-value">' + daysLeft + '</span>' +
            '<span class="timer-sub-label">روز مانده</span>' +
          '</div>' +
        '</div>' +
        '<div class="expire-date">انقضا: ' + esc(expStr) + '</div>' +
      '</div>';
  }

  // ── Subscription URL ──────────────────────────────────────────────────
  var subUrlEl = document.getElementById('sub-url-text');
  subUrlEl.textContent = subUrl;
  subUrlEl.addEventListener('click', function() {
    copyText(subUrl).then(function() { showToast('لینک سابسکریپشن کپی شد', true); })
                    .catch(function() { showToast('', false); });
  });
  document.getElementById('copy-sub-btn').addEventListener('click', function() {
    copyText(subUrl).then(function() { showToast('لینک سابسکریپشن کپی شد', true); })
                    .catch(function() { showToast('', false); });
  });

  // ── Connection Links ──────────────────────────────────────────────────
  var container = document.getElementById('links-container');
  var titleEl   = document.getElementById('links-title');
  if (links.length === 0) {
    titleEl.textContent = '\u{1F517} لینک‌های اتصال';
    container.innerHTML = '<div class="links-empty">⚠️ لینک فعالی وجود ندارد</div>';
  } else {
    titleEl.textContent = '\u{1F517} لینک‌های اتصال (' + links.length + ')';
    links.forEach(function(link, i) {
      var name  = linkName(link, i);
      var p     = proto(link);
      var short = link.length > 72 ? link.substring(0, 72) + '…' : link;

      var card = document.createElement('div');
      card.className = 'link-card';
      card.style.marginBottom = '10px';
      card.innerHTML =
        '<span class="link-proto-badge">' + esc(p) + '</span>' +
        '<div class="link-card-body">' +
          '<div class="link-name">' + esc(name) + '</div>' +
          '<div class="link-uri-short">' + esc(short) + '</div>' +
        '</div>' +
        '<span class="link-copy-icon">⧉</span>';

      card.addEventListener('click', function() {
        copyText(link)
          .then(function() { showToast(name, true); })
          .catch(function() { showToast('', false); });
      });
      container.appendChild(card);
    });
  }

  // ── Platform tabs ─────────────────────────────────────────────────────
  document.querySelectorAll('.platform-tab').forEach(function(btn) {
    btn.addEventListener('click', function() {
      document.querySelectorAll('.platform-tab').forEach(function(b) { b.classList.remove('active'); });
      document.querySelectorAll('.platform-panel').forEach(function(p) { p.classList.remove('active'); });
      btn.classList.add('active');
      var target = document.getElementById(btn.getAttribute('data-target'));
      if (target) target.classList.add('active');
    });
  });

  // ── Theme ─────────────────────────────────────────────────────────────
  if (localStorage.getItem('subpanel-theme') === 'light') document.body.classList.add('light');
  document.getElementById('theme-toggle').addEventListener('click', function() {
    document.body.classList.toggle('light');
    localStorage.setItem('subpanel-theme', document.body.classList.contains('light') ? 'light' : 'dark');
  });

  // ── Spotlight ─────────────────────────────────────────────────────────
  document.querySelectorAll('.glass').forEach(function(card) {
    var spot = card.querySelector('.spotlight');
    if (!spot) return;
    card.addEventListener('mousemove', function(e) {
      var r = card.getBoundingClientRect();
      spot.style.background = 'radial-gradient(circle 180px at '+(e.clientX-r.left)+'px '+(e.clientY-r.top)+'px,rgba(255,255,255,.08),transparent 70%)';
    });
    card.addEventListener('mouseleave', function() { spot.style.background = 'none'; });
  });

  // ── Particles ─────────────────────────────────────────────────────────
  (function(){
    var c = document.getElementById('particles-canvas');
    var ctx = c.getContext('2d'), W, H, pts = [], mx = -999, my = -999;
    function resize() { W = c.width = innerWidth; H = c.height = innerHeight; }
    resize(); addEventListener('resize', resize);
    addEventListener('mousemove', function(e) { mx = e.clientX; my = e.clientY; });
    var COLS = ['rgba(96,165,250,A)','rgba(167,139,250,A)','rgba(34,211,238,A)','rgba(129,140,248,A)'];
    function P() {
      this.x = Math.random()*W; this.y = Math.random()*H;
      this.sz = Math.random()*1.8+0.4; this.bsz = this.sz;
      this.vx = (Math.random()-.5)*.3; this.vy = (Math.random()-.5)*.3;
      this.col = COLS[Math.floor(Math.random()*COLS.length)];
      this.a = Math.random()*.4+.1; this.ba = this.a;
      this.ph = Math.random()*Math.PI*2; this.ps = Math.random()*.02+.005;
    }
    P.prototype.tick = function() {
      this.ph += this.ps; this.a = this.ba + Math.sin(this.ph)*.03;
      var dx = this.x-mx, dy = this.y-my, d = Math.sqrt(dx*dx+dy*dy);
      if (d < 120) { var f=(120-d)/120; this.a=Math.min(1,this.ba+f*.5); this.sz=this.bsz+f*2; this.x+=dx*f*.007; this.y+=dy*f*.007; }
      else this.sz = this.bsz;
      this.x += this.vx; this.y += this.vy;
      if (this.x < -10) this.x = W+10; if (this.x > W+10) this.x = -10;
      if (this.y < -10) this.y = H+10; if (this.y > H+10) this.y = -10;
    };
    P.prototype.draw = function() {
      var a = Math.max(0,Math.min(1,this.a));
      ctx.beginPath(); ctx.arc(this.x,this.y,this.sz,0,Math.PI*2);
      ctx.fillStyle = this.col.replace('A',a.toFixed(2)); ctx.fill();
    };
    var n = Math.min(60, Math.floor(innerWidth*innerHeight/20000));
    for (var i=0;i<n;i++) pts.push(new P());
    function loop() {
      ctx.clearRect(0,0,W,H);
      pts.forEach(function(p){ p.tick(); p.draw(); });
      requestAnimationFrame(loop);
    }
    loop();
  })();

})();
</script>
</body></html>
`
