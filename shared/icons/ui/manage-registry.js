import attachmentSemantic from '../desktop/attachment.svg?raw'
import fileSemantic from '../desktop/file.svg?raw'
import toolsSemantic from '../desktop/tools.svg?raw'
import sendSemantic from '../desktop/send.svg?raw'
import browserSemantic from '../desktop/browser.svg?raw'
import agentSemantic from '../desktop/agent.svg?raw'
import workgroupSemantic from '../desktop/nav-workgroups.svg?raw'
import autoSemantic from '../desktop/auto.svg?raw'
import skillsSemantic from '../lucide/puzzle.svg?raw'
import icon53 from './close.svg?raw'
import icon54 from './check.svg?raw'
import icon55 from './pending.svg?raw'
import icon56 from './transfer.svg?raw'
import icon57 from './alert.svg?raw'
import icon58 from './plus.svg?raw'
import icon59 from './chevron-right.svg?raw'
import icon60 from './chevron-left.svg?raw'
import icon61 from './chevron-up.svg?raw'
import icon62 from './chevron-down.svg?raw'
import icon63 from './trash.svg?raw'
import icon64 from './arrow-left.svg?raw'
import icon65 from './arrow-up.svg?raw'
import icon66 from './arrow-down.svg?raw'
import icon68 from './image.svg?raw'
import icon70 from './file-plus.svg?raw'
import icon71 from './receipt-text.svg?raw'
import icon72 from './circle-plus.svg?raw'
import icon73 from './code-2.svg?raw'
import icon74 from './briefcase.svg?raw'
import icon75 from './package.svg?raw'
import icon76 from './panel-right.svg?raw'
import icon78 from './pencil.svg?raw'
import icon79 from './more-horizontal.svg?raw'
import icon80 from './square.svg?raw'
import icon81 from './refresh-cw.svg?raw'
import icon82 from './settings.svg?raw'
import icon83 from './monitor.svg?raw'
import icon84 from './sun.svg?raw'
import icon85 from './moon.svg?raw'
import icon86 from './terminal.svg?raw'
import icon87 from './terminal.svg?raw'
import icon88 from './message-square.svg?raw'
import icon89 from './message-square.svg?raw'
import icon90 from './bot.svg?raw'
import icon91 from './sparkles.svg?raw'
import icon92 from './clock.svg?raw'
import icon93 from './shield.svg?raw'
import icon94 from './shield-check.svg?raw'
import icon95 from './brain.svg?raw'
import icon96 from './info.svg?raw'
import icon97 from './network.svg?raw'
import icon98 from './plug.svg?raw'
import icon99 from './server.svg?raw'
import icon100 from './users.svg?raw'
import icon101 from './globe.svg?raw'
import icon103 from './bookmark.svg?raw'
import icon105 from './node-fallback.svg?raw'
import wecomSemantic from './wecom.svg?raw'

const body = svg => svg.replace(/^[\s\S]*?<svg\b[^>]*>/, '').replace(/<\/svg>\s*$/, '').trim()

export const manageIconNames = ["attachment","browser","agent","workgroup","auto","skills","close","check","pending","transfer","alert","plus","chevron-right","chevron-left","chevron-up","chevron-down","trash","arrow-left","arrow-up","arrow-down","paperclip","image","file","file-plus","receipt-text","circle-plus","code-2","briefcase","package","panel-right","send","pencil","more-horizontal","square","refresh-cw","settings","monitor","sun","moon","terminal","square-terminal","message-square","message-circle","bot","sparkles","clock","shield","shield-check","brain","info","network","plug","server","users","globe","wrench","wecom","bookmark","tool"]
export const manageIconRegistry = Object.freeze({
  "close": body(icon53),
  "check": body(icon54),
  "pending": body(icon55),
  "transfer": body(icon56),
  "alert": body(icon57),
  "plus": body(icon58),
  "chevron-right": body(icon59),
  "chevron-left": body(icon60),
  "chevron-up": body(icon61),
  "chevron-down": body(icon62),
  "trash": body(icon63),
  "arrow-left": body(icon64),
  "arrow-up": body(icon65),
  "arrow-down": body(icon66),
  "image": body(icon68),
  "file-plus": body(icon70),
  "receipt-text": body(icon71),
  "circle-plus": body(icon72),
  "code-2": body(icon73),
  "briefcase": body(icon74),
  "package": body(icon75),
  "panel-right": body(icon76),
  "pencil": body(icon78),
  "more-horizontal": body(icon79),
  "square": body(icon80),
  "refresh-cw": body(icon81),
  "settings": body(icon82),
  "monitor": body(icon83),
  "sun": body(icon84),
  "moon": body(icon85),
  "terminal": body(icon86),
  "square-terminal": body(icon87),
  "message-square": body(icon88),
  "message-circle": body(icon89),
  "bot": body(icon90),
  "sparkles": body(icon91),
  "clock": body(icon92),
  "shield": body(icon93),
  "shield-check": body(icon94),
  "brain": body(icon95),
  "info": body(icon96),
  "network": body(icon97),
  "plug": body(icon98),
  "server": body(icon99),
  "users": body(icon100),
  "globe": body(icon101),
  "bookmark": body(icon103),
  "agent": agentSemantic,
  "workgroup": workgroupSemantic,
  "auto": autoSemantic,
  "skills": skillsSemantic,
  "browser": browserSemantic,
  "attachment": attachmentSemantic,
  "paperclip": attachmentSemantic,
  "file": fileSemantic,
  "send": sendSemantic,
  "wrench": toolsSemantic,
  "tool": toolsSemantic,
  "wecom": wecomSemantic,
  __fallback: body(icon105),
})
export const resolveManageIcon = name => manageIconRegistry[name] || manageIconRegistry.__fallback
