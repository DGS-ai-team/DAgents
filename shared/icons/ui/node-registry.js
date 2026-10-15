import attachmentSemantic from '../desktop/attachment.svg?raw'
import fileSemantic from '../desktop/file.svg?raw'
import toolsSemantic from '../desktop/tools.svg?raw'
import sendSemantic from '../desktop/send.svg?raw'
import browserSemantic from '../desktop/browser.svg?raw'
import agentSemantic from '../desktop/agent.svg?raw'
import workgroupSemantic from '../desktop/nav-workgroups.svg?raw'
import autoSemantic from '../desktop/auto.svg?raw'
import skillsSemantic from '../lucide/puzzle.svg?raw'
import icon0 from './close.svg?raw'
import icon1 from './check.svg?raw'
import icon2 from './pending.svg?raw'
import icon3 from './transfer.svg?raw'
import icon4 from './alert.svg?raw'
import icon5 from './plus.svg?raw'
import icon6 from './chevron-right.svg?raw'
import icon7 from './chevron-left.svg?raw'
import icon8 from './chevron-up.svg?raw'
import icon9 from './chevron-down.svg?raw'
import icon10 from './trash.svg?raw'
import icon11 from './arrow-left.svg?raw'
import icon12 from './arrow-up.svg?raw'
import icon13 from './arrow-down.svg?raw'
import icon15 from './image.svg?raw'
import icon17 from './file-plus.svg?raw'
import icon18 from './receipt-text.svg?raw'
import icon19 from './circle-plus.svg?raw'
import icon20 from './code-2.svg?raw'
import icon21 from './briefcase.svg?raw'
import icon22 from './package.svg?raw'
import icon23 from './panel-right.svg?raw'
import icon25 from './pencil.svg?raw'
import icon26 from './more-horizontal.svg?raw'
import icon27 from './square.svg?raw'
import icon28 from './refresh-cw.svg?raw'
import icon29 from './settings.svg?raw'
import icon30 from './monitor.svg?raw'
import icon31 from './sun.svg?raw'
import icon32 from './moon.svg?raw'
import icon33 from './terminal.svg?raw'
import icon34 from './terminal.svg?raw'
import icon35 from './message-square.svg?raw'
import icon36 from './message-square.svg?raw'
import icon37 from './bot.svg?raw'
import icon38 from './sparkles.svg?raw'
import icon39 from './clock.svg?raw'
import icon40 from './shield.svg?raw'
import icon41 from './shield-check.svg?raw'
import icon42 from './brain.svg?raw'
import icon43 from './info.svg?raw'
import icon44 from './network.svg?raw'
import icon45 from './plug.svg?raw'
import icon46 from './server.svg?raw'
import icon47 from './users.svg?raw'
import icon48 from './globe.svg?raw'
import icon50 from './bookmark.svg?raw'
import icon52 from './node-fallback.svg?raw'

const body = svg => svg.replace(/^[\s\S]*?<svg\b[^>]*>/, '').replace(/<\/svg>\s*$/, '').trim()

export const nodeIconNames = ["attachment","browser","agent","workgroup","auto","skills","close","check","pending","transfer","alert","plus","chevron-right","chevron-left","chevron-up","chevron-down","trash","arrow-left","arrow-up","arrow-down","paperclip","image","file","file-plus","receipt-text","circle-plus","code-2","briefcase","package","panel-right","send","pencil","more-horizontal","square","refresh-cw","settings","monitor","sun","moon","terminal","square-terminal","message-square","message-circle","bot","sparkles","clock","shield","shield-check","brain","info","network","plug","server","users","globe","wrench","bookmark","tool"]
export const nodeIconRegistry = Object.freeze({
  "close": body(icon0),
  "check": body(icon1),
  "pending": body(icon2),
  "transfer": body(icon3),
  "alert": body(icon4),
  "plus": body(icon5),
  "chevron-right": body(icon6),
  "chevron-left": body(icon7),
  "chevron-up": body(icon8),
  "chevron-down": body(icon9),
  "trash": body(icon10),
  "arrow-left": body(icon11),
  "arrow-up": body(icon12),
  "arrow-down": body(icon13),
  "image": body(icon15),
  "file-plus": body(icon17),
  "receipt-text": body(icon18),
  "circle-plus": body(icon19),
  "code-2": body(icon20),
  "briefcase": body(icon21),
  "package": body(icon22),
  "panel-right": body(icon23),
  "pencil": body(icon25),
  "more-horizontal": body(icon26),
  "square": body(icon27),
  "refresh-cw": body(icon28),
  "settings": body(icon29),
  "monitor": body(icon30),
  "sun": body(icon31),
  "moon": body(icon32),
  "terminal": body(icon33),
  "square-terminal": body(icon34),
  "message-square": body(icon35),
  "message-circle": body(icon36),
  "bot": body(icon37),
  "sparkles": body(icon38),
  "clock": body(icon39),
  "shield": body(icon40),
  "shield-check": body(icon41),
  "brain": body(icon42),
  "info": body(icon43),
  "network": body(icon44),
  "plug": body(icon45),
  "server": body(icon46),
  "users": body(icon47),
  "globe": body(icon48),
  "bookmark": body(icon50),
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
  __fallback: body(icon52),
})
export const resolveNodeIcon = name => nodeIconRegistry[name] || nodeIconRegistry.__fallback
