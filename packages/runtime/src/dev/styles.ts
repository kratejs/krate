// Overlay styles, ported from the approved mockup. Colors are tokens so the
// light theme only swaps the token set.

const DARK = `
--kd-bg:#101013;--kd-raise:#16161a;--kd-raise2:#1d1d22;--kd-hover:rgba(255,255,255,.05);
--kd-line:rgba(255,255,255,.05);--kd-line2:rgba(255,255,255,.085);--kd-line0:rgba(255,255,255,.03);
--kd-fg:#ededf0;--kd-fg2:#a6a6b0;--kd-fg3:#6e6e78;--kd-strong:#fff;
--kd-accent:#fa7814;--kd-accent-soft:rgba(250,120,20,.14);--kd-accent-line:rgba(250,120,20,.26);
--kd-accent-fg:#ffb37a;--kd-busy-fg:#ffc391;--kd-accent-hover:#ff8a2e;--kd-on-accent:#1a0d02;
--kd-ok:#3ddc97;--kd-ok-soft:rgba(61,220,151,.13);--kd-ok-fg:#9af0c8;
--kd-hint-bg:rgba(61,220,151,.07);--kd-hint-line:rgba(61,220,151,.22);--kd-hint-fg:#c6f3df;
--kd-err:#ff6b6b;--kd-err-soft:rgba(255,107,107,.12);--kd-err-line:rgba(255,107,107,.24);
--kd-err-fg:#ffb3b3;--kd-err-fg2:#ff9d9d;--kd-err-tint:rgba(255,107,107,.05);--kd-err-tint2:rgba(255,107,107,.13);
--kd-warn:#fbbf24;--kd-warn-fg:#fde68a;--kd-warn-fg2:#fcd77a;--kd-warn-tint:rgba(251,191,36,.05);
--kd-blue:#6cb2ff;--kd-blue-soft:rgba(108,178,255,.15);--kd-blue-fg:#9ccbff;
--kd-violet:#b197ff;--kd-violet-soft:rgba(177,151,255,.16);--kd-violet-fg:#cdbcff;
--kd-pink:#f472b6;--kd-pink-soft:rgba(244,114,182,.15);--kd-pink-fg:#f9a8d4;
--kd-static-soft:rgba(156,163,175,.16);--kd-static-fg:#c4c8cf;--kd-chip:rgba(255,255,255,.08);
--kd-glass:rgba(16,16,19,.92);--kd-glass2:rgba(16,16,19,.94);
--kd-code-bg:#0a0a0c;--kd-term-bg:#08080a;--kd-code-fg:#cfd0d6;--kd-log-fg:#c7c8cf;--kd-ln:#4b4b54;--kd-spark:#3a3a43;
--kd-scrim:rgba(8,8,10,.62);--kd-scrim2:rgba(8,8,10,.42);--kd-sh:rgba(0,0,0,.35);--kd-sh2:rgba(0,0,0,.4);--kd-sh3:rgba(0,0,0,.5);
--kd-tint:rgba(255,255,255,.012);--kd-tint2:rgba(255,255,255,.02);--kd-idle:rgba(255,255,255,.06);
--k1:#ff9e64;--k2:#7dcfff;--k3:#9ece6a;--k4:#bb9af7;--k5:#6b6b78;--k6:#e0af68;
--t-static:#8b929c;--t-client:#fa7814;--t-server:#3d8ff0;--t-runtime:#8c6cf0;--t-island:#e0457b;
color-scheme:dark;`;

const LIGHT = `
--kd-bg:#ffffff;--kd-raise:#f6f6f8;--kd-raise2:#ededf0;--kd-hover:rgba(0,0,0,.045);
--kd-line:rgba(0,0,0,.06);--kd-line2:rgba(0,0,0,.1);--kd-line0:rgba(0,0,0,.035);
--kd-fg:#18181b;--kd-fg2:#52525b;--kd-fg3:#8a8a94;--kd-strong:#09090b;
--kd-accent:#ea6c0c;--kd-accent-soft:rgba(234,108,12,.1);--kd-accent-line:rgba(234,108,12,.28);
--kd-accent-fg:#c2530a;--kd-busy-fg:#b45309;--kd-accent-hover:#f97316;--kd-on-accent:#fff;
--kd-ok:#16a34a;--kd-ok-soft:rgba(22,163,74,.1);--kd-ok-fg:#15803d;
--kd-hint-bg:rgba(22,163,74,.06);--kd-hint-line:rgba(22,163,74,.2);--kd-hint-fg:#166534;
--kd-err:#dc2626;--kd-err-soft:rgba(220,38,38,.08);--kd-err-line:rgba(220,38,38,.22);
--kd-err-fg:#b91c1c;--kd-err-fg2:#dc2626;--kd-err-tint:rgba(220,38,38,.04);--kd-err-tint2:rgba(220,38,38,.09);
--kd-warn:#d97706;--kd-warn-fg:#92400e;--kd-warn-fg2:#b45309;--kd-warn-tint:rgba(217,119,6,.05);
--kd-blue:#2563eb;--kd-blue-soft:rgba(37,99,235,.1);--kd-blue-fg:#1d4ed8;
--kd-violet:#7c3aed;--kd-violet-soft:rgba(124,58,237,.1);--kd-violet-fg:#6d28d9;
--kd-pink:#db2777;--kd-pink-soft:rgba(219,39,119,.1);--kd-pink-fg:#be185d;
--kd-static-soft:rgba(107,114,128,.12);--kd-static-fg:#4b5563;--kd-chip:rgba(0,0,0,.06);
--kd-glass:rgba(255,255,255,.9);--kd-glass2:rgba(255,255,255,.94);
--kd-code-bg:#f8f8fa;--kd-term-bg:#f6f6f8;--kd-code-fg:#27272a;--kd-log-fg:#3f3f46;--kd-ln:#a1a1aa;--kd-spark:#d4d4d8;
--kd-scrim:rgba(24,24,27,.28);--kd-scrim2:rgba(24,24,27,.16);--kd-sh:rgba(0,0,0,.1);--kd-sh2:rgba(0,0,0,.06);--kd-sh3:rgba(0,0,0,.16);
--kd-tint:rgba(0,0,0,.015);--kd-tint2:rgba(0,0,0,.02);--kd-idle:rgba(0,0,0,.05);
--k1:#c2410c;--k2:#0369a1;--k3:#15803d;--k4:#7c3aed;--k5:#71717a;--k6:#a16207;
--t-static:#6b7280;--t-client:#ea6c0c;--t-server:#2563eb;--t-runtime:#7c3aed;--t-island:#db2777;
color-scheme:light;`;

export const STYLE = `
:host{all:initial;position:fixed;top:0;left:0;width:0;height:0;overflow:visible;z-index:2147483000;pointer-events:none;--sans:ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;--mono:ui-monospace,"SF Mono",SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace;${DARK}}
:host([data-theme=light]){${LIGHT}}
*{box-sizing:border-box}
[hidden]{display:none!important}
.kd{font:13px/1.45 var(--sans);color:var(--kd-fg);-webkit-font-smoothing:antialiased;pointer-events:none;text-align:left}
.kd .mono,.kd code,.kd kbd,.mono{font-family:var(--mono)}
.dock,.panel,.toast,.ov,.pal-wrap,.legend{pointer-events:auto}
button{font:inherit;color:inherit;background:none;border:0;padding:0;margin:0;cursor:pointer;text-align:inherit}
input,select{font:inherit;color:inherit}
button:focus-visible,[tabindex]:focus-visible,input:focus-visible,select:focus-visible{outline:2px solid var(--kd-accent);outline-offset:1px}
svg.i{width:15px;height:15px;flex:none;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.mark{width:18px;height:18px;color:var(--kd-accent);flex:none;display:block}
.grow{flex:1}
.link{color:var(--kd-blue);cursor:pointer}
.link:hover{text-decoration:underline}
.kbd{display:inline-flex;align-items:center;gap:1px;font:500 10.5px/1 var(--mono);color:var(--kd-fg3);border:1px solid var(--kd-line2);border-bottom-width:2px;border-radius:4px;padding:3px 5px;white-space:nowrap}
.kbd svg.i{width:10.5px;height:10.5px;stroke-width:2.2}
.mode{font:600 10px/1 var(--sans);letter-spacing:.06em;padding:3px 5px;border-radius:4px;background:var(--kd-chip);color:var(--kd-fg2);flex:none}
.mode.ssr,.mode.streaming,.mode.api{background:var(--kd-blue-soft);color:var(--kd-blue)}
.mode.isr{background:var(--kd-violet-soft);color:var(--kd-violet)}

.dock{position:fixed;z-index:4;display:flex;align-items:center;height:38px;padding:0 5px 0 10px;border-radius:12px;background:var(--kd-glass);backdrop-filter:blur(10px);-webkit-backdrop-filter:blur(10px);border:1px solid var(--kd-line2);box-shadow:0 10px 30px var(--kd-sh),0 0 0 1px var(--kd-sh2);overflow:hidden;user-select:none;touch-action:none;max-width:calc(100vw - 32px)}
.c-bl .dock{left:16px;bottom:16px}.c-br .dock{right:16px;bottom:16px}.c-tl .dock{left:16px;top:16px}.c-tr .dock{right:16px;top:16px}
.dock.dragging{transition:none;cursor:grabbing;box-shadow:0 18px 40px var(--kd-sh3)}
.dock .markbtn{display:grid;place-items:center;margin-right:10px;border-radius:6px}
.dock .seg{display:flex;align-items:center;gap:7px;height:28px;padding:0 9px;border-radius:8px;color:var(--kd-fg2);white-space:nowrap;min-width:0}
.dock button.seg:hover,.dock .seg.open{background:var(--kd-hover);color:var(--kd-fg)}
.dock .sep{width:1px;height:18px;background:var(--kd-line2);margin:0 3px;flex:none}
.dock .route{color:var(--kd-fg);font-size:12px;overflow:hidden;text-overflow:ellipsis;max-width:280px}
.dock .seg.ok{background:var(--kd-ok-soft);color:var(--kd-ok-fg)}
.dock button.seg.ok:hover{background:var(--kd-ok-soft);color:var(--kd-ok-fg);filter:brightness(1.12)}
.dock .seg.busy{background:var(--kd-accent-soft);color:var(--kd-busy-fg)}
.dock .seg.idle{background:var(--kd-idle)}
.dock .seg.on{background:var(--kd-accent-soft);color:var(--kd-accent-fg)}
.dock .seg.on .kbd{color:var(--kd-accent-fg);border-color:var(--kd-accent-line)}
.dock .ms{font-size:12px}
.badge{display:inline-flex;align-items:center;gap:5px;font-weight:600;font-size:12px;padding:3px 8px;border-radius:7px}
.badge.err{background:var(--kd-err-soft);color:var(--kd-err);border:1px solid var(--kd-err-line)}
.dock button.seg:hover .badge.err{filter:brightness(1.1)}
.dock.error{border-color:var(--kd-err-line);box-shadow:0 10px 30px var(--kd-sh),0 0 0 3px rgba(255,107,107,.10)}
.dock.building::after{content:"";position:absolute;left:0;bottom:0;height:2px;width:40%;background:linear-gradient(90deg,transparent,var(--kd-accent),transparent);animation:sweep 1.3s linear infinite}
.dock.inspect{border-color:var(--kd-accent-line)}
.dock.off .mark{color:var(--kd-fg3)}
.dock.mini{padding:0;width:38px;justify-content:center}
.dock.mini .markbtn{margin:0}
.dock .pip{position:absolute;top:6px;right:6px;width:8px;height:8px;border-radius:50%;background:var(--kd-err);border:2px solid var(--kd-bg)}
.dock .pip.w{background:var(--kd-warn)}
.badge.warn{background:rgba(251,191,36,.1);color:var(--kd-warn);border:1px solid rgba(251,191,36,.26)}
.dock button.seg:hover .badge.warn{filter:brightness(1.1)}
.dock.warn{border-color:rgba(251,191,36,.24)}
.dock .seg.stale{background:rgba(251,191,36,.08);color:var(--kd-warn-fg2);font-size:12px}
.spin{width:12px;height:12px;border-radius:50%;border:2px solid var(--kd-line2);border-top-color:var(--kd-accent);animation:rot .8s linear infinite;flex:none}
@keyframes sweep{from{transform:translateX(-100%)}to{transform:translateX(260%)}}
@keyframes rot{to{transform:rotate(360deg)}}

.panel{position:fixed;z-index:5;width:520px;max-width:calc(100vw - 32px);max-height:min(654px,calc(100vh - 96px));display:flex;flex-direction:column;border-radius:14px;background:var(--kd-bg);border:1px solid var(--kd-line2);box-shadow:0 24px 60px var(--kd-sh3);overflow:hidden}
.panel.wide{width:620px}
.c-bl .panel,.c-bl .toast{left:16px;bottom:64px}.c-br .panel,.c-br .toast{right:16px;bottom:64px}
.c-tl .panel,.c-tl .toast{left:16px;top:64px}.c-tr .panel,.c-tr .toast{right:16px;top:64px}
.p-head{display:flex;align-items:center;gap:10px;padding:12px 14px 10px}
.p-head .t{font-weight:600}
.ver{font:500 11px var(--mono);color:var(--kd-fg3)}
.conn{display:flex;align-items:center;gap:6px;font-size:12px;font-weight:500;padding:3px 8px;border-radius:6px;background:var(--kd-ok-soft);color:var(--kd-ok-fg)}
.conn.busy{background:var(--kd-accent-soft);color:var(--kd-busy-fg)}
.conn.off{background:var(--kd-idle);color:var(--kd-fg2)}
.iconbtn{display:inline-grid;place-items:center;width:26px;height:26px;border-radius:7px;color:var(--kd-fg3);flex:none}
.iconbtn:hover{background:var(--kd-hover);color:var(--kd-fg)}
.iconbtn:disabled{opacity:.4;cursor:default;background:none}
.tabs{display:flex;gap:2px;padding:0 8px;box-shadow:inset 0 -1px 0 var(--kd-line);overflow-x:auto;overflow-y:hidden;scrollbar-width:none;flex:none}
.tab{display:flex;align-items:center;gap:6px;padding:8px 9px 8px;color:var(--kd-fg3);font-weight:500;font-size:12.5px;border-bottom:2px solid transparent;white-space:nowrap}
.tab:hover{color:var(--kd-fg2)}
.tab[aria-selected=true]{color:var(--kd-fg);border-bottom-color:var(--kd-accent)}
.tab .n{font:600 10px/1 var(--sans);background:var(--kd-err-soft);color:var(--kd-err);padding:2px 5px;border-radius:5px}
.p-body{overflow:auto;padding:14px;min-height:0}
.sect{margin-bottom:16px}
.sect:last-child{margin-bottom:0}
.lbl{font-size:12.5px;font-weight:500;color:var(--kd-fg2);margin-bottom:8px;display:flex;align-items:center;gap:8px}
.kv{display:grid;grid-template-columns:96px minmax(0,1fr);gap:7px 12px;align-items:center;margin:0}
.kv dt{color:var(--kd-fg3)}
.kv dd{margin:0;display:flex;align-items:center;gap:8px;min-width:0;flex-wrap:wrap}
.kv dd .mono{font-size:12px;color:var(--kd-fg);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:100%}
.kv dd .mono.link{color:var(--kd-blue)}
.kv dd .dim{color:var(--kd-fg2)}
.chip{display:inline-flex;align-items:center;gap:5px;font-size:11.5px;padding:2px 7px;border-radius:6px;background:var(--kd-raise2);border:1px solid var(--kd-line);color:var(--kd-fg2);white-space:nowrap}
.chip .k{color:var(--kd-fg3)}
.chip svg.i{width:13px;height:13px}
.chip.ok svg.i{color:var(--kd-ok)}
.chip.warn{color:var(--kd-warn)}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:8px}
.stat{background:var(--kd-raise);border:1px solid var(--kd-line);border-radius:10px;padding:9px 10px;min-width:0}
.stat .k{font-size:11px;color:var(--kd-fg3)}
.stat .v{font:600 16px/1.3 var(--sans);letter-spacing:-.01em;margin-top:2px;white-space:nowrap}
.stat .v small{font-size:11px;font-weight:500;color:var(--kd-fg3)}
.bar{height:6px;border-radius:99px;background:var(--kd-raise2);overflow:hidden;display:flex}
.bar i{display:block;height:100%}
.budget{display:flex;justify-content:space-between;gap:12px;font-size:12px;color:var(--kd-fg2);margin-top:7px}
.budget .over{color:var(--kd-err)}
.bgts{margin-top:12px;display:grid;gap:6px}
.bgt{display:grid;grid-template-columns:84px minmax(0,1fr) auto;gap:10px;align-items:center;font-size:12px;color:var(--kd-fg2)}
.bgt .bar{height:4px}
.bgt .bar i{background:var(--kd-ok)}
.bgt.over .bar i{background:var(--kd-err)}
.bgt.over .v{color:var(--kd-err)}
.bgt-note{font-size:11.5px;color:var(--kd-fg3);line-height:1.4;margin-top:2px}
.rows{border:1px solid var(--kd-line);border-radius:10px;overflow:hidden}
.row{display:flex;align-items:center;gap:10px;padding:8px 10px;border-top:1px solid var(--kd-line);width:100%;white-space:nowrap}
.row:first-child{border-top:0}
.row:hover{background:var(--kd-hover)}
.row .name{font-size:12px;color:var(--kd-fg);flex:none}
.row .sub{color:var(--kd-fg3);font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0;flex:1 1 auto}
.row .right{margin-left:auto;display:flex;align-items:center;gap:8px;white-space:nowrap;color:var(--kd-fg2);font-size:12px;flex:none}
.row .right .dim{color:var(--kd-fg3)}
.tp{font:600 10.5px/1 var(--sans);padding:3px 6px;border-radius:5px;white-space:nowrap;flex:none}
.tp.static{background:var(--kd-static-soft);color:var(--kd-static-fg)}.tp.client{background:var(--kd-accent-soft);color:var(--kd-accent-fg)}
.tp.server{background:var(--kd-blue-soft);color:var(--kd-blue-fg)}.tp.runtime{background:var(--kd-violet-soft);color:var(--kd-violet-fg)}
.tp.island{background:var(--kd-pink-soft);color:var(--kd-pink-fg)}
.actions{display:flex;flex-wrap:wrap;gap:6px}
.b{display:inline-flex;align-items:center;gap:7px;height:30px;padding:0 11px;border-radius:8px;border:1px solid var(--kd-line2);background:var(--kd-raise);color:var(--kd-fg);font-weight:500;font-size:12.5px;white-space:nowrap}
.b:hover{background:var(--kd-raise2)}
.b:disabled{opacity:.5;cursor:default}
.b.pri{background:var(--kd-accent);border-color:var(--kd-accent);color:var(--kd-on-accent)}
.b.pri:hover{background:var(--kd-accent-hover)}
.b.ghost{background:none;border-color:transparent;color:var(--kd-fg2)}
.b.ghost:hover{background:var(--kd-hover);color:var(--kd-fg)}
.b.ghost[aria-pressed=true]{background:var(--kd-err-soft);color:var(--kd-err)}
.b .kbd{margin-left:2px}
.b.pri .kbd{color:rgba(26,13,2,.6);border-color:rgba(26,13,2,.25)}
.empty{color:var(--kd-fg3);font-size:12.5px;padding:6px 0}
.note{color:var(--kd-fg3);font-size:12px;margin-top:8px}

.ov{position:fixed;inset:0;z-index:8;background:var(--kd-scrim);backdrop-filter:blur(5px) saturate(.8);-webkit-backdrop-filter:blur(5px) saturate(.8);display:flex;justify-content:center;align-items:flex-start;padding:34px 24px}
.modal{width:1060px;max-width:100%;max-height:100%;display:flex;flex-direction:column;border-radius:16px;background:var(--kd-bg);border:1px solid var(--kd-line2);box-shadow:0 40px 100px var(--kd-sh3);overflow:hidden}
.modal:focus,.pal:focus{outline:none}
.m-head{display:flex;align-items:center;gap:12px;padding:14px 18px;border-bottom:1px solid var(--kd-line);background:linear-gradient(180deg,rgba(255,107,107,.07),transparent);flex:none}
.m-head.warn{background:linear-gradient(180deg,rgba(250,120,20,.08),transparent)}
.m-head .mark{width:22px;height:22px}
.m-title{font-size:15px;font-weight:600;letter-spacing:-.01em}
.m-title.red{color:var(--kd-err)}
.m-title.amber{color:var(--kd-warn)}
.m-sub{color:var(--kd-fg3);font-size:12.5px}
.pager{display:flex;align-items:center;gap:2px;border:1px solid var(--kd-line2);border-radius:8px;padding:2px}
.pager span{font-size:12px;color:var(--kd-fg2);padding:0 8px;font-variant-numeric:tabular-nums;white-space:nowrap}
.pager .iconbtn{width:24px;height:24px}
.m-body{display:grid;grid-template-columns:270px minmax(0,1fr);min-height:0;flex:1}
.m-body.solo{grid-template-columns:minmax(0,1fr)}
.rail{border-right:1px solid var(--kd-line);padding:10px;overflow:auto;background:var(--kd-tint)}
.rail .lbl{padding:4px 6px 0}
.rail .lbl+.issue{margin-top:0}
.rail .lbl:not(:first-child){margin-top:12px}
.issue{display:block;width:100%;padding:9px 10px;border-radius:9px;margin-bottom:3px;border:1px solid transparent}
.issue .top1{display:flex;align-items:center;gap:7px;font-size:11.5px;color:var(--kd-fg3);margin-bottom:3px}
.issue .top1 .cnt{margin-left:auto}
.issue .msg{font-size:12.5px;color:var(--kd-fg);line-height:1.35;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;word-break:break-word}
.issue .msg code{font-size:12px}
.issue .file{font:11.5px var(--mono);color:var(--kd-fg3);margin-top:4px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.issue.e{background:var(--kd-err-tint)}.issue.e .top1{color:var(--kd-err-fg2)}
.issue.w{background:var(--kd-warn-tint)}.issue.w .top1{color:var(--kd-warn-fg2)}
.issue.e:hover,.issue.w:hover{filter:brightness(1.15)}
.issue.e[aria-current=true]{background:var(--kd-err-tint2);border-color:var(--kd-err-line)}
.issue.w[aria-current=true]{background:rgba(251,191,36,.11);border-color:rgba(251,191,36,.26)}
.detail{padding:20px 24px;overflow:auto;min-width:0}
.d-meta{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:10px}
.code{font:600 11.5px var(--mono);padding:3px 7px;border-radius:6px;background:var(--kd-err-soft);color:var(--kd-err);border:1px solid var(--kd-err-line)}
.code.w{background:rgba(251,191,36,.1);color:var(--kd-warn);border-color:rgba(251,191,36,.26)}
.whatbtn{font-size:12.5px;color:var(--kd-accent-fg)}
.whatbtn:hover{text-decoration:underline}
.codepop{position:relative;margin:-2px 0 12px;padding:11px 13px;border-radius:10px;background:var(--kd-raise);border:1px solid var(--kd-line2);box-shadow:0 10px 30px var(--kd-sh);color:var(--kd-fg2);font-size:12.5px;line-height:1.5}
.codepop .t{color:var(--kd-fg);font-weight:600;margin-bottom:4px;padding-right:22px}
.codepop code{background:var(--kd-chip);padding:0 5px;border-radius:4px;font-size:12px;color:var(--kd-fg)}
.codepop .x{position:absolute;top:7px;right:7px}
.codepop a{display:inline-block;margin-top:8px;color:var(--kd-accent-fg);text-decoration:none}
.codepop a:hover{text-decoration:underline}
.cf.w .cf-l.hl{background:rgba(251,191,36,.08);box-shadow:inset 2px 0 0 var(--kd-warn)}
.cf.w .cf-l.hl .n,.cf.w .cf-l.ann .c{color:var(--kd-warn)}
.cf.w .sq{text-decoration-color:var(--kd-warn)}
.reqs{border:1px solid var(--kd-line);border-radius:10px;overflow:hidden}
.req{display:grid;grid-template-columns:62px 52px minmax(0,1fr) auto auto;gap:12px;align-items:baseline;padding:7px 12px;border-top:1px solid var(--kd-line);font:12px var(--mono)}
.req:first-child{border-top:0}
.req .st{color:var(--kd-err-fg)}
.req .m{color:var(--kd-fg2)}
.req .u{color:var(--kd-fg);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.req .ms,.req .ts{color:var(--kd-fg3)}
.req .why{grid-column:3/-1;color:var(--kd-fg3);font-size:11.5px;margin-top:-3px}
.d-msg{font-size:19px;font-weight:600;letter-spacing:-.015em;line-height:1.35;margin:0 0 14px;color:var(--kd-strong);word-break:break-word;white-space:pre-wrap}
.d-msg.long{font-size:15px;line-height:1.45}
.d-msg code{font-size:17px;background:var(--kd-chip);padding:1px 6px;border-radius:5px;font-weight:500}
.fileline{display:flex;align-items:center;gap:10px;margin-bottom:10px;flex-wrap:wrap}
.fileline .path{font:12.5px var(--mono);color:var(--kd-fg2);word-break:break-all}
.fileline .path b{color:var(--kd-fg);font-weight:500}
.cf{border:1px solid var(--kd-line);border-radius:11px;background:var(--kd-code-bg);overflow:auto;font:12.5px/1.75 var(--mono)}
.cf-bar{display:flex;align-items:center;gap:8px;padding:6px 12px;border-bottom:1px solid var(--kd-line);color:var(--kd-fg3);font-size:11.5px;background:var(--kd-tint2);position:sticky;left:0}
.cf-l{display:flex;white-space:pre;min-width:max-content}
.cf-l .n{width:46px;padding-right:14px;text-align:right;color:var(--kd-ln);user-select:none;flex:none}
.cf-l .c{color:var(--kd-code-fg);padding-right:14px}
.cf-l.hl{background:rgba(255,107,107,.09);box-shadow:inset 2px 0 0 var(--kd-err)}
.cf-l.hl .n{color:var(--kd-err)}
.cf-l.ann .c{color:var(--kd-err);font-size:12px}
.sq{text-decoration:underline wavy var(--kd-err);text-underline-offset:4px;text-decoration-thickness:1.5px}
.k1{color:var(--k1)}.k2{color:var(--k2)}.k3{color:var(--k3)}.k4{color:var(--k4)}.k5{color:var(--k5)}.k6{color:var(--k6)}
.hint{display:flex;gap:10px;margin-top:14px;padding:11px 13px;border-radius:10px;background:var(--kd-hint-bg);border:1px solid var(--kd-hint-line);color:var(--kd-hint-fg)}
.hint svg.i{color:var(--kd-ok);margin-top:2px}
.hint code{color:var(--kd-strong);background:var(--kd-chip);padding:0 5px;border-radius:4px;font-size:12px}
.fix{margin-top:12px;border:1px solid var(--kd-accent-line);border-radius:11px;overflow:hidden}
.fix-h{display:flex;align-items:center;gap:10px;padding:9px 12px;background:var(--kd-accent-soft);flex-wrap:wrap}
.fix-h .t{font-weight:600;color:var(--kd-busy-fg)}
.fix-h .s{color:var(--kd-fg2);font-size:12px}
.fix-h svg.i{color:var(--kd-accent)}
.diff{font:12.5px/1.7 var(--mono);background:var(--kd-code-bg);overflow:auto}
.diff div{padding:0 12px;white-space:pre}
.diff .del{background:rgba(255,107,107,.08);color:var(--kd-err-fg)}
.diff .add{background:rgba(61,220,151,.08);color:var(--kd-ok-fg)}
.m-foot{display:flex;align-items:center;gap:12px;padding:10px 18px;border-top:1px solid var(--kd-line);color:var(--kd-fg3);font-size:12px;background:var(--kd-tint2);flex:none;flex-wrap:wrap}
.m-foot .sp{display:flex;align-items:center;gap:7px}
.subtabs{display:flex;gap:4px;margin:16px 0 12px;border-bottom:1px solid var(--kd-line)}
.subtabs button{padding:7px 10px;font-size:12.5px;font-weight:500;color:var(--kd-fg3);border-bottom:2px solid transparent;margin-bottom:-1px}
.subtabs button[aria-selected=true]{color:var(--kd-fg);border-bottom-color:var(--kd-fg)}
.subtabs span{font-size:11px;color:var(--kd-fg3);margin-left:4px}
.frames{border:1px solid var(--kd-line);border-radius:10px;overflow:hidden}
.fr{display:flex;align-items:baseline;gap:12px;padding:8px 12px;border-top:1px solid var(--kd-line);width:100%;min-width:0}
.fr:first-child{border-top:0}
.fr .fn{font:500 12.5px var(--mono);color:var(--kd-fg);min-width:180px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.fr .at{font:12px var(--mono);color:var(--kd-fg3);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}
.fr .at b{color:var(--kd-blue);font-weight:400}
.fr.app{background:rgba(250,120,20,.08)}
button.fr.app:hover{background:rgba(250,120,20,.13)}
.fr.fw .fn{color:var(--kd-fg2);font-weight:400}
.fr.fold{color:var(--kd-fg3);font-size:12px;justify-content:space-between;align-items:center}
.fr.fold:hover{background:var(--kd-hover)}
.crumbs{display:flex;align-items:center;flex-wrap:wrap;gap:4px;font:12.5px var(--mono);color:var(--kd-fg2);margin-bottom:14px}
.crumbs .sepc{color:var(--kd-fg3)}
.crumbs .cur{color:var(--kd-err);background:var(--kd-err-soft);padding:1px 6px;border-radius:5px}
.term{border:1px solid var(--kd-line);border-radius:11px;background:var(--kd-term-bg);font:12px/1.7 var(--mono);overflow:auto;max-height:300px}
.term .tl{display:flex;gap:12px;padding:0 12px;white-space:pre;min-width:max-content}
.term .ts{color:var(--kd-ln)}
.term .src{width:62px;flex:none}
.s-krate{color:var(--kd-accent)}.s-ssr{color:var(--kd-blue)}.s-api,.s-server,.s-middleware{color:var(--kd-violet)}.s-browser,.s-page{color:var(--kd-ok)}.s-go{color:var(--k2)}
.term .e{color:var(--kd-err-fg)}.term .w{color:var(--kd-warn-fg)}.term .m{color:var(--kd-log-fg)}
.term .tl.hl{background:rgba(255,107,107,.08)}
.stackraw{margin:0;padding:10px 12px;font:12px/1.6 var(--mono);color:var(--kd-fg2);white-space:pre-wrap;word-break:break-word}

.ins{position:fixed;inset:0;z-index:1;pointer-events:none;overflow:hidden}
.ob{position:fixed;outline:1.5px dashed var(--tc);outline-offset:3px;border-radius:4px}
.ob.hot{outline-style:solid;outline-width:2px;background:color-mix(in srgb,var(--tc) 7%,transparent)}
.ob .nm{position:absolute;top:-21px;left:-2px;font:600 10.5px/1 var(--mono);color:#fff;background:var(--tc);padding:4px 6px;border-radius:4px 4px 4px 0;white-space:nowrap}
.ob.flip .nm{left:auto;right:-2px;border-radius:4px 4px 0 4px}
.ob.inside .nm{top:0;left:0;border-radius:0 0 4px 0}
.ob.inside.flip .nm{left:auto;right:0;border-radius:0 0 0 4px}
.ob.static{--tc:var(--t-static)}.ob.client{--tc:var(--t-client)}.ob.server{--tc:var(--t-server)}.ob.runtime{--tc:var(--t-runtime)}.ob.island{--tc:var(--t-island)}
.legend{position:fixed;top:16px;right:16px;z-index:6;display:flex;align-items:center;gap:6px;padding:8px 8px 8px 14px;border-radius:12px;background:var(--kd-glass2);backdrop-filter:blur(10px);-webkit-backdrop-filter:blur(10px);border:1px solid var(--kd-line2);box-shadow:0 12px 30px var(--kd-sh)}
.c-tr .legend{right:auto;left:16px}
.legend .li{display:flex;align-items:center;gap:6px;font-size:12px;font-weight:500;padding:5px 9px;border-radius:7px}
.legend .li b{color:inherit;font-weight:700;margin-left:4px}
.legend .sep{width:1px;height:18px;background:var(--kd-line2)}
.legend .js{color:var(--kd-fg2);font-size:12px;white-space:nowrap}
.tip{position:fixed;z-index:7;width:340px;max-width:calc(100vw - 16px);border-radius:12px;background:var(--kd-bg);border:1px solid var(--kd-line2);box-shadow:0 20px 50px var(--kd-sh2);overflow:hidden;pointer-events:none}
.tip-h{display:flex;align-items:center;gap:8px;padding:10px 12px;border-bottom:1px solid var(--kd-line)}
.tip-h .name{font:600 13px var(--mono);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tip-h .tp{margin-left:auto}
.tip-b{padding:10px 12px}
.tip-b .kv{grid-template-columns:84px minmax(0,1fr);font-size:12.5px}
.tip-b .kv dd .mono{white-space:normal;overflow-wrap:anywhere}
.tip-b .kv dd .mono.path{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;direction:rtl;text-align:left}
.tip-f{padding:8px 12px;border-top:1px solid var(--kd-line);color:var(--kd-fg3);font-size:11.5px;display:flex;gap:12px}
.sv-n{color:var(--kd-accent)}.sv-b{color:var(--kd-blue)}.sv-s{color:var(--kd-ok)}
.tog{margin-left:auto;width:32px;height:19px;border-radius:99px;background:var(--kd-raise2);border:1px solid var(--kd-line2);position:relative;flex:none}
.tog::after{content:"";position:absolute;top:2px;left:2px;width:13px;height:13px;border-radius:50%;background:var(--kd-fg3);transition:left .15s ease}
.tog[aria-checked=true]{background:var(--kd-accent);border-color:var(--kd-accent)}
.tog[aria-checked=true]::after{left:15px;background:#fff}
.tiers .row{cursor:default}
.tiers .row .sub{white-space:normal}

.ev{display:grid;grid-template-columns:54px 18px minmax(0,1fr) auto;gap:0 10px;padding:10px 0;border-top:1px solid var(--kd-line);align-items:start;width:100%}
.ev:first-child{border-top:0;padding-top:0}
.ev .time{font:12px var(--mono);color:var(--kd-fg3);padding-top:1px}
.ev .ic{padding-top:1px}
.ev .ic svg.i{display:block}
.ev .what{min-width:0}
.ev .what .h{font-weight:500;overflow:hidden;text-overflow:ellipsis}
.ev .what .h code{font-size:12px;color:var(--kd-fg2);background:var(--kd-raise2);padding:1px 5px;border-radius:4px}
.ev .what .s{color:var(--kd-fg3);font-size:12px;margin-top:2px}
.ev .dur{font:500 12px var(--mono);color:var(--kd-fg2)}
.ev.click{cursor:pointer}
.phases{margin-top:9px}
.phases .bar{height:8px;gap:2px;background:none}
.phases .bar i{border-radius:2px;min-width:2px}
.plegend{display:flex;flex-wrap:wrap;gap:4px 12px;margin-top:7px;font-size:11.5px;color:var(--kd-fg3)}
.plegend span{display:flex;align-items:center;gap:5px}
.plegend i{width:8px;height:8px;border-radius:2px;display:block}
.spark{display:flex;align-items:flex-end;gap:3px;height:44px}
.spark i{flex:1;background:var(--kd-spark);border-radius:2px 2px 0 0;min-height:2px}
.spark i.hi{background:var(--kd-accent)}
.spark i.bad{background:var(--kd-err)}
.actgrid{display:grid;grid-template-columns:minmax(0,1fr) 220px;gap:16px;align-items:end}

.toolbar2{display:flex;align-items:center;gap:8px;margin-bottom:10px}
.segc{display:flex;padding:2px;border-radius:8px;background:var(--kd-raise);border:1px solid var(--kd-line);width:max-content;flex:none}
.segc button{padding:4px 9px;border-radius:6px;font-size:12px;color:var(--kd-fg3);font-weight:500}
.segc button:hover{color:var(--kd-fg2)}
.segc button[aria-pressed=true]{background:var(--kd-raise2);color:var(--kd-fg);box-shadow:0 1px 0 rgba(255,255,255,.05) inset}
.search{flex:1;display:flex;align-items:center;gap:7px;height:30px;padding:0 10px;border-radius:8px;border:1px solid var(--kd-line);background:var(--kd-raise);color:var(--kd-fg3);font-size:12.5px;min-width:0}
.search input{flex:1;min-width:0;border:0;background:none;outline:none;color:var(--kd-fg);font-size:12.5px;padding:0}
.search input::placeholder{color:var(--kd-fg3)}
.search:focus-within{border-color:var(--kd-line2)}
.log{font:12px/1.6 var(--mono);border:1px solid var(--kd-line);border-radius:10px;overflow:auto;background:var(--kd-code-bg);min-height:120px;flex:1}
.p-body.logs{display:flex;flex-direction:column;overflow:hidden}
.lg{display:grid;grid-template-columns:62px 58px minmax(0,1fr);gap:10px;padding:4px 12px;border-top:1px solid var(--kd-line0)}
.lg:first-child{border-top:0}
.lg .src{font-size:11px;font-weight:600;padding-top:1px}
.lg.e{background:rgba(255,107,107,.06)}.lg.e .m{color:var(--kd-err-fg)}
.lg.w{background:rgba(251,191,36,.05)}.lg.w .m{color:var(--kd-warn-fg)}
.lg .m{color:var(--kd-log-fg);white-space:pre-wrap;word-break:break-word}
.lg .ts{color:var(--kd-ln)}
.lg .more{color:var(--kd-fg3);margin-top:2px;display:block}
.lg .more:hover{color:var(--kd-fg2)}

.pal-wrap{position:fixed;inset:0;z-index:9;background:var(--kd-scrim2);backdrop-filter:blur(2px);-webkit-backdrop-filter:blur(2px);display:flex;justify-content:center;align-items:flex-start;padding:110px 16px 16px}
.pal{width:640px;max-width:100%;max-height:calc(100vh - 140px);display:flex;flex-direction:column;border-radius:16px;background:var(--kd-bg);border:1px solid var(--kd-line2);box-shadow:0 40px 100px var(--kd-sh3);overflow:hidden}
.pal-in{display:flex;align-items:center;gap:10px;padding:14px 16px;border-bottom:1px solid var(--kd-line);font-size:15px;flex:none}
.pal-in svg.i{color:var(--kd-fg3);width:18px;height:18px}
.pal-in input{flex:1;min-width:0;border:0;background:none;outline:none;font-size:15px;color:var(--kd-fg);caret-color:var(--kd-accent);padding:0}
.pal-in input::placeholder{color:var(--kd-fg3)}
.pal-list{overflow:auto;min-height:0}
.pal-g{padding:6px}
.pal-g+.pal-g{border-top:1px solid var(--kd-line)}
.pal-g .lbl{padding:8px 10px 4px;margin:0}
.pi{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:9px;color:var(--kd-fg2);width:100%;min-width:0}
.pi[aria-selected=true]{background:var(--kd-raise2);color:var(--kd-fg)}
.pi .name{color:var(--kd-fg);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}
.pi .name.mono{font-size:12.5px}
.pi .name mark{background:none;color:var(--kd-accent);font-weight:600}
.pi .r{margin-left:auto;display:flex;gap:6px;align-items:center;color:var(--kd-fg3);font-size:12px;white-space:nowrap;flex:none}
.pi.sub{padding-left:34px}
.pi .ent{display:none}.pi[aria-selected=true] .ent{display:inline-flex}
.pal-f{display:flex;gap:16px;padding:9px 16px;border-top:1px solid var(--kd-line);color:var(--kd-fg3);font-size:12px;flex:none;flex-wrap:wrap}
.pal-f span{display:flex;align-items:center;gap:6px}
.pal-empty{padding:18px 16px;color:var(--kd-fg3);font-size:12.5px}

.corner{display:grid;grid-template-columns:1fr 1fr;gap:6px;width:150px;padding:6px;border-radius:10px;border:1px solid var(--kd-line);background:var(--kd-raise);flex:none}
.corner button{height:34px;border-radius:6px;border:1px dashed var(--kd-line2);position:relative}
.corner button:hover{background:var(--kd-hover)}
.corner button[aria-pressed=true]{border:1px solid var(--kd-accent-line);background:var(--kd-accent-soft)}
.corner button[aria-pressed=true]::after{content:"";position:absolute;width:30px;height:8px;border-radius:3px;background:var(--kd-accent)}
.corner button.bl::after{left:6px;bottom:6px}.corner button.br::after{right:6px;bottom:6px}
.corner button.tl::after{left:6px;top:6px}.corner button.tr::after{right:6px;top:6px}
.setrow{display:flex;align-items:center;gap:12px;padding:10px 0;border-top:1px solid var(--kd-line)}
.setrow:first-child{border-top:0}
.setrow .t{font-weight:500}
.setrow .d{font-size:12px;color:var(--kd-fg3)}
.select{margin-left:auto;white-space:nowrap;flex:none;display:flex;align-items:center;gap:8px;height:30px;padding:0 10px;border-radius:8px;border:1px solid var(--kd-line2);background:var(--kd-raise);font-size:12.5px;position:relative}
.select select{position:absolute;inset:0;opacity:0;cursor:pointer;width:100%}
.select .cmd{color:var(--kd-fg3);font-size:11.5px}
.select svg.i{width:13px;height:13px}
.keys{display:grid;grid-template-columns:1fr auto;gap:8px 12px;font-size:12.5px;color:var(--kd-fg2)}
.keys span:nth-child(even){display:flex;gap:4px;justify-content:flex-end}

.hostrows{font:12px/1.6 var(--mono);border:1px solid var(--kd-line);border-radius:10px;background:var(--kd-code-bg);max-height:420px;overflow:auto;padding:4px 0}
.hostrow{padding:2px 12px;white-space:pre-wrap;word-break:break-word;color:var(--kd-log-fg)}
.hostrow.err{color:var(--kd-err-fg);background:rgba(255,107,107,.06)}
.hostrow.sig{color:var(--kd-fg3)}

.toast{position:fixed;z-index:5;display:flex;align-items:center;gap:10px;padding:9px 12px 9px 10px;border-radius:12px;background:var(--kd-glass);backdrop-filter:blur(10px);-webkit-backdrop-filter:blur(10px);border:1px solid var(--kd-line2);box-shadow:0 10px 30px var(--kd-sh),0 0 0 1px var(--kd-sh2);font-size:12.5px;max-width:calc(100vw - 32px)}
.toast .ok{color:var(--kd-ok)}
.toast .x{color:var(--kd-err)}
.toast .meta{color:var(--kd-fg3);font-size:12px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}

@media (max-width:760px){
  .m-body{grid-template-columns:minmax(0,1fr)}
  .rail{border-right:0;border-bottom:1px solid var(--kd-line);max-height:170px}
  .ov{padding:12px}
  .actgrid{grid-template-columns:1fr}
  .stats{grid-template-columns:repeat(2,1fr)}
}
@media (prefers-reduced-motion: reduce){
  *,*::before,*::after{animation-duration:.001ms!important;animation-iteration-count:1!important;transition:none!important}
  .spin{animation:rot 1.6s linear infinite!important}
}
`;
