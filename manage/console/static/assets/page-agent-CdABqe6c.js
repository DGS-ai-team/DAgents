var Ot=Object.defineProperty;var it=e=>{throw TypeError(e)};var At=(e,t,n)=>t in e?Ot(e,t,{enumerable:!0,configurable:!0,writable:!0,value:n}):e[t]=n;var R=(e,t,n)=>At(e,typeof t!="symbol"?t+"":t,n),Ke=(e,t,n)=>t.has(e)||it("Cannot "+n);var p=(e,t,n)=>(Ke(e,t,"read from private field"),n?n.call(e):t.get(e)),L=(e,t,n)=>t.has(e)?it("Cannot add the same private member more than once"):t instanceof WeakSet?t.add(e):t.set(e,n),I=(e,t,n,r)=>(Ke(e,t,"write to private field"),r?r.call(e,n):t.set(e,n),n),w=(e,t,n)=>(Ke(e,t,"access private method"),n);import{_ as __vitePreload}from"./index-CbLuSfU2.js";var _a$1;function $constructor(e,t,n){function r(a,l){if(a._zod||Object.defineProperty(a,"_zod",{value:{def:l,constr:i,traits:new Set},enumerable:!1}),a._zod.traits.has(e))return;a._zod.traits.add(e),t(a,l);const d=i.prototype,u=Object.keys(d);for(let g=0;g<u.length;g++){const h=u[g];h in a||(a[h]=d[h].bind(a))}}const o=(n==null?void 0:n.Parent)??Object;class s extends o{}Object.defineProperty(s,"name",{value:e});function i(a){var l;const d=n!=null&&n.Parent?new s:this;r(d,a),(l=d._zod).deferred??(l.deferred=[]);for(const u of d._zod.deferred)u();return d}return Object.defineProperty(i,"init",{value:r}),Object.defineProperty(i,Symbol.hasInstance,{value:a=>{var l,d;return n!=null&&n.Parent&&a instanceof n.Parent?!0:(d=(l=a==null?void 0:a._zod)==null?void 0:l.traits)==null?void 0:d.has(e)}}),Object.defineProperty(i,"name",{value:e}),i}class $ZodAsyncError extends Error{constructor(){super("Encountered Promise during synchronous parse. Use .parseAsync() instead.")}}class $ZodEncodeError extends Error{constructor(t){super(`Encountered unidirectional transform during encode: ${t}`),this.name="ZodEncodeError"}}(_a$1=globalThis).__zod_globalConfig??(_a$1.__zod_globalConfig={});const globalConfig=globalThis.__zod_globalConfig;function config(e){return globalConfig}function getEnumValues(e){const t=Object.values(e).filter(r=>typeof r=="number");return Object.entries(e).filter(([r,o])=>t.indexOf(+r)===-1).map(([r,o])=>o)}function jsonStringifyReplacer(e,t){return typeof t=="bigint"?t.toString():t}function cached(e){return{get value(){{const t=e();return Object.defineProperty(this,"value",{value:t}),t}}}}function nullish(e){return e==null}function cleanRegex(e){const t=e.startsWith("^")?1:0,n=e.endsWith("$")?e.length-1:e.length;return e.slice(t,n)}function floatSafeRemainder(e,t){const n=e/t,r=Math.round(n),o=Number.EPSILON*Math.max(Math.abs(n),1);return Math.abs(n-r)<o?0:n-r}const EVALUATING=Symbol("evaluating");function defineLazy(e,t,n){let r;Object.defineProperty(e,t,{get(){if(r!==EVALUATING)return r===void 0&&(r=EVALUATING,r=n()),r},set(o){Object.defineProperty(e,t,{value:o})},configurable:!0})}function assignProp(e,t,n){Object.defineProperty(e,t,{value:n,writable:!0,enumerable:!0,configurable:!0})}function mergeDefs(...e){const t={};for(const n of e){const r=Object.getOwnPropertyDescriptors(n);Object.assign(t,r)}return Object.defineProperties({},t)}function esc(e){return JSON.stringify(e)}function slugify(e){return e.toLowerCase().trim().replace(/[^\w\s-]/g,"").replace(/[\s_-]+/g,"-").replace(/^-+|-+$/g,"")}const captureStackTrace="captureStackTrace"in Error?Error.captureStackTrace:(...e)=>{};function isObject(e){return typeof e=="object"&&e!==null&&!Array.isArray(e)}const allowsEval=cached(()=>{var e;if(globalConfig.jitless||typeof navigator<"u"&&((e=navigator==null?void 0:navigator.userAgent)!=null&&e.includes("Cloudflare")))return!1;try{const t=Function;return new t(""),!0}catch{return!1}});function isPlainObject(e){if(isObject(e)===!1)return!1;const t=e.constructor;if(t===void 0||typeof t!="function")return!0;const n=t.prototype;return!(isObject(n)===!1||Object.prototype.hasOwnProperty.call(n,"isPrototypeOf")===!1)}function shallowClone(e){return isPlainObject(e)?{...e}:Array.isArray(e)?[...e]:e instanceof Map?new Map(e):e instanceof Set?new Set(e):e}const propertyKeyTypes=new Set(["string","number","symbol"]);function escapeRegex(e){return e.replace(/[.*+?^${}()|[\]\\]/g,"\\$&")}function clone(e,t,n){const r=new e._zod.constr(t??e._zod.def);return(!t||n!=null&&n.parent)&&(r._zod.parent=e),r}function normalizeParams(e){const t=e;if(!t)return{};if(typeof t=="string")return{error:()=>t};if((t==null?void 0:t.message)!==void 0){if((t==null?void 0:t.error)!==void 0)throw new Error("Cannot specify both `message` and `error` params");t.error=t.message}return delete t.message,typeof t.error=="string"?{...t,error:()=>t.error}:t}function optionalKeys(e){return Object.keys(e).filter(t=>e[t]._zod.optin==="optional"&&e[t]._zod.optout==="optional")}const NUMBER_FORMAT_RANGES={safeint:[Number.MIN_SAFE_INTEGER,Number.MAX_SAFE_INTEGER],int32:[-2147483648,2147483647],uint32:[0,4294967295],float32:[-34028234663852886e22,34028234663852886e22],float64:[-Number.MAX_VALUE,Number.MAX_VALUE]};function pick(e,t){const n=e._zod.def,r=n.checks;if(r&&r.length>0)throw new Error(".pick() cannot be used on object schemas containing refinements");const s=mergeDefs(e._zod.def,{get shape(){const i={};for(const a in t){if(!(a in n.shape))throw new Error(`Unrecognized key: "${a}"`);t[a]&&(i[a]=n.shape[a])}return assignProp(this,"shape",i),i},checks:[]});return clone(e,s)}function omit(e,t){const n=e._zod.def,r=n.checks;if(r&&r.length>0)throw new Error(".omit() cannot be used on object schemas containing refinements");const s=mergeDefs(e._zod.def,{get shape(){const i={...e._zod.def.shape};for(const a in t){if(!(a in n.shape))throw new Error(`Unrecognized key: "${a}"`);t[a]&&delete i[a]}return assignProp(this,"shape",i),i},checks:[]});return clone(e,s)}function extend(e,t){if(!isPlainObject(t))throw new Error("Invalid input to extend: expected a plain object");const n=e._zod.def.checks;if(n&&n.length>0){const s=e._zod.def.shape;for(const i in t)if(Object.getOwnPropertyDescriptor(s,i)!==void 0)throw new Error("Cannot overwrite keys on object schemas containing refinements. Use `.safeExtend()` instead.")}const o=mergeDefs(e._zod.def,{get shape(){const s={...e._zod.def.shape,...t};return assignProp(this,"shape",s),s}});return clone(e,o)}function safeExtend(e,t){if(!isPlainObject(t))throw new Error("Invalid input to safeExtend: expected a plain object");const n=mergeDefs(e._zod.def,{get shape(){const r={...e._zod.def.shape,...t};return assignProp(this,"shape",r),r}});return clone(e,n)}function merge(e,t){var r;if((r=e._zod.def.checks)!=null&&r.length)throw new Error(".merge() cannot be used on object schemas containing refinements. Use .safeExtend() instead.");const n=mergeDefs(e._zod.def,{get shape(){const o={...e._zod.def.shape,...t._zod.def.shape};return assignProp(this,"shape",o),o},get catchall(){return t._zod.def.catchall},checks:t._zod.def.checks??[]});return clone(e,n)}function partial(e,t,n){const o=t._zod.def.checks;if(o&&o.length>0)throw new Error(".partial() cannot be used on object schemas containing refinements");const i=mergeDefs(t._zod.def,{get shape(){const a=t._zod.def.shape,l={...a};if(n)for(const d in n){if(!(d in a))throw new Error(`Unrecognized key: "${d}"`);n[d]&&(l[d]=e?new e({type:"optional",innerType:a[d]}):a[d])}else for(const d in a)l[d]=e?new e({type:"optional",innerType:a[d]}):a[d];return assignProp(this,"shape",l),l},checks:[]});return clone(t,i)}function required(e,t,n){const r=mergeDefs(t._zod.def,{get shape(){const o=t._zod.def.shape,s={...o};if(n)for(const i in n){if(!(i in s))throw new Error(`Unrecognized key: "${i}"`);n[i]&&(s[i]=new e({type:"nonoptional",innerType:o[i]}))}else for(const i in o)s[i]=new e({type:"nonoptional",innerType:o[i]});return assignProp(this,"shape",s),s}});return clone(t,r)}function aborted(e,t=0){var n;if(e.aborted===!0)return!0;for(let r=t;r<e.issues.length;r++)if(((n=e.issues[r])==null?void 0:n.continue)!==!0)return!0;return!1}function explicitlyAborted(e,t=0){var n;if(e.aborted===!0)return!0;for(let r=t;r<e.issues.length;r++)if(((n=e.issues[r])==null?void 0:n.continue)===!1)return!0;return!1}function prefixIssues(e,t){return t.map(n=>{var r;return(r=n).path??(r.path=[]),n.path.unshift(e),n})}function unwrapMessage(e){return typeof e=="string"?e:e==null?void 0:e.message}function finalizeIssue(e,t,n){var l,d,u,g,h,m;const r=e.message?e.message:unwrapMessage((u=(d=(l=e.inst)==null?void 0:l._zod.def)==null?void 0:d.error)==null?void 0:u.call(d,e))??unwrapMessage((g=t==null?void 0:t.error)==null?void 0:g.call(t,e))??unwrapMessage((h=n.customError)==null?void 0:h.call(n,e))??unwrapMessage((m=n.localeError)==null?void 0:m.call(n,e))??"Invalid input",{inst:o,continue:s,input:i,...a}=e;return a.path??(a.path=[]),a.message=r,t!=null&&t.reportInput&&(a.input=i),a}function getLengthableOrigin(e){return Array.isArray(e)?"array":typeof e=="string"?"string":"unknown"}function issue(...e){const[t,n,r]=e;return typeof t=="string"?{message:t,code:"custom",input:n,inst:r}:{...t}}const initializer$1=(e,t)=>{e.name="$ZodError",Object.defineProperty(e,"_zod",{value:e._zod,enumerable:!1}),Object.defineProperty(e,"issues",{value:t,enumerable:!1}),e.message=JSON.stringify(t,jsonStringifyReplacer,2),Object.defineProperty(e,"toString",{value:()=>e.message,enumerable:!1})},$ZodError=$constructor("$ZodError",initializer$1),$ZodRealError=$constructor("$ZodError",initializer$1,{Parent:Error});function flattenError(e,t=n=>n.message){const n={},r=[];for(const o of e.issues)o.path.length>0?(n[o.path[0]]=n[o.path[0]]||[],n[o.path[0]].push(t(o))):r.push(t(o));return{formErrors:r,fieldErrors:n}}function formatError(e,t=n=>n.message){const n={_errors:[]},r=(o,s=[])=>{for(const i of o.issues)if(i.code==="invalid_union"&&i.errors.length)i.errors.map(a=>r({issues:a},[...s,...i.path]));else if(i.code==="invalid_key")r({issues:i.issues},[...s,...i.path]);else if(i.code==="invalid_element")r({issues:i.issues},[...s,...i.path]);else{const a=[...s,...i.path];if(a.length===0)n._errors.push(t(i));else{let l=n,d=0;for(;d<a.length;){const u=a[d];d===a.length-1?(l[u]=l[u]||{_errors:[]},l[u]._errors.push(t(i))):l[u]=l[u]||{_errors:[]},l=l[u],d++}}}};return r(e),n}function toDotPath(e){const t=[],n=e.map(r=>typeof r=="object"?r.key:r);for(const r of n)typeof r=="number"?t.push(`[${r}]`):typeof r=="symbol"?t.push(`[${JSON.stringify(String(r))}]`):/[^\w$]/.test(r)?t.push(`[${JSON.stringify(r)}]`):(t.length&&t.push("."),t.push(r));return t.join("")}function prettifyError(e){var r;const t=[],n=[...e.issues].sort((o,s)=>(o.path??[]).length-(s.path??[]).length);for(const o of n)t.push(`✖ ${o.message}`),(r=o.path)!=null&&r.length&&t.push(`  → at ${toDotPath(o.path)}`);return t.join(`
`)}const _parse=e=>(t,n,r,o)=>{const s=r?{...r,async:!1}:{async:!1},i=t._zod.run({value:n,issues:[]},s);if(i instanceof Promise)throw new $ZodAsyncError;if(i.issues.length){const a=new((o==null?void 0:o.Err)??e)(i.issues.map(l=>finalizeIssue(l,s,config())));throw captureStackTrace(a,o==null?void 0:o.callee),a}return i.value},_parseAsync=e=>async(t,n,r,o)=>{const s=r?{...r,async:!0}:{async:!0};let i=t._zod.run({value:n,issues:[]},s);if(i instanceof Promise&&(i=await i),i.issues.length){const a=new((o==null?void 0:o.Err)??e)(i.issues.map(l=>finalizeIssue(l,s,config())));throw captureStackTrace(a,o==null?void 0:o.callee),a}return i.value},_safeParse=e=>(t,n,r)=>{const o=r?{...r,async:!1}:{async:!1},s=t._zod.run({value:n,issues:[]},o);if(s instanceof Promise)throw new $ZodAsyncError;return s.issues.length?{success:!1,error:new(e??$ZodError)(s.issues.map(i=>finalizeIssue(i,o,config())))}:{success:!0,data:s.value}},safeParse$1=_safeParse($ZodRealError),_safeParseAsync=e=>async(t,n,r)=>{const o=r?{...r,async:!0}:{async:!0};let s=t._zod.run({value:n,issues:[]},o);return s instanceof Promise&&(s=await s),s.issues.length?{success:!1,error:new e(s.issues.map(i=>finalizeIssue(i,o,config())))}:{success:!0,data:s.value}},safeParseAsync$1=_safeParseAsync($ZodRealError),_encode=e=>(t,n,r)=>{const o=r?{...r,direction:"backward"}:{direction:"backward"};return _parse(e)(t,n,o)},_decode=e=>(t,n,r)=>_parse(e)(t,n,r),_encodeAsync=e=>async(t,n,r)=>{const o=r?{...r,direction:"backward"}:{direction:"backward"};return _parseAsync(e)(t,n,o)},_decodeAsync=e=>async(t,n,r)=>_parseAsync(e)(t,n,r),_safeEncode=e=>(t,n,r)=>{const o=r?{...r,direction:"backward"}:{direction:"backward"};return _safeParse(e)(t,n,o)},_safeDecode=e=>(t,n,r)=>_safeParse(e)(t,n,r),_safeEncodeAsync=e=>async(t,n,r)=>{const o=r?{...r,direction:"backward"}:{direction:"backward"};return _safeParseAsync(e)(t,n,o)},_safeDecodeAsync=e=>async(t,n,r)=>_safeParseAsync(e)(t,n,r),cuid=/^[cC][0-9a-z]{6,}$/,cuid2=/^[0-9a-z]+$/,ulid=/^[0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{26}$/,xid=/^[0-9a-vA-V]{20}$/,ksuid=/^[A-Za-z0-9]{27}$/,nanoid=/^[a-zA-Z0-9_-]{21}$/,duration$1=/^P(?:(\d+W)|(?!.*W)(?=\d|T\d)(\d+Y)?(\d+M)?(\d+D)?(T(?=\d)(\d+H)?(\d+M)?(\d+([.,]\d+)?S)?)?)$/,guid=/^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/,uuid=e=>e?new RegExp(`^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-${e}[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12})$`):/^([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}|00000000-0000-0000-0000-000000000000|ffffffff-ffff-ffff-ffff-ffffffffffff)$/,email=/^(?!\.)(?!.*\.\.)([A-Za-z0-9_'+\-\.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$/,_emoji$1="^(\\p{Extended_Pictographic}|\\p{Emoji_Component})+$";function emoji(){return new RegExp(_emoji$1,"u")}const ipv4=/^(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.){3}(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])$/,ipv6=/^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:))$/,cidrv4=/^((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\/([0-9]|[1-2][0-9]|3[0-2])$/,cidrv6=/^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|::|([0-9a-fA-F]{1,4})?::([0-9a-fA-F]{1,4}:?){0,6})\/(12[0-8]|1[01][0-9]|[1-9]?[0-9])$/,base64=/^$|^(?:[0-9a-zA-Z+/]{4})*(?:(?:[0-9a-zA-Z+/]{2}==)|(?:[0-9a-zA-Z+/]{3}=))?$/,base64url=/^[A-Za-z0-9_-]*$/,httpProtocol=/^https?$/,e164=/^\+[1-9]\d{6,14}$/,dateSource="(?:(?:\\d\\d[2468][048]|\\d\\d[13579][26]|\\d\\d0[48]|[02468][048]00|[13579][26]00)-02-29|\\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\\d|30)|(?:02)-(?:0[1-9]|1\\d|2[0-8])))",date$1=new RegExp(`^${dateSource}$`);function timeSource(e){const t="(?:[01]\\d|2[0-3]):[0-5]\\d";return typeof e.precision=="number"?e.precision===-1?`${t}`:e.precision===0?`${t}:[0-5]\\d`:`${t}:[0-5]\\d\\.\\d{${e.precision}}`:`${t}(?::[0-5]\\d(?:\\.\\d+)?)?`}function time$1(e){return new RegExp(`^${timeSource(e)}$`)}function datetime$1(e){const t=timeSource({precision:e.precision}),n=["Z"];e.local&&n.push(""),e.offset&&n.push("([+-](?:[01]\\d|2[0-3]):[0-5]\\d)");const r=`${t}(?:${n.join("|")})`;return new RegExp(`^${dateSource}T(?:${r})$`)}const string$1=e=>{const t=e?`[\\s\\S]{${(e==null?void 0:e.minimum)??0},${(e==null?void 0:e.maximum)??""}}`:"[\\s\\S]*";return new RegExp(`^${t}$`)},integer=/^-?\d+$/,number$1=/^-?\d+(?:\.\d+)?$/,boolean$1=/^(?:true|false)$/i,lowercase=/^[^A-Z]*$/,uppercase=/^[^a-z]*$/,$ZodCheck=$constructor("$ZodCheck",(e,t)=>{var n;e._zod??(e._zod={}),e._zod.def=t,(n=e._zod).onattach??(n.onattach=[])}),numericOriginMap={number:"number",bigint:"bigint",object:"date"},$ZodCheckLessThan=$constructor("$ZodCheckLessThan",(e,t)=>{$ZodCheck.init(e,t);const n=numericOriginMap[typeof t.value];e._zod.onattach.push(r=>{const o=r._zod.bag,s=(t.inclusive?o.maximum:o.exclusiveMaximum)??Number.POSITIVE_INFINITY;t.value<s&&(t.inclusive?o.maximum=t.value:o.exclusiveMaximum=t.value)}),e._zod.check=r=>{(t.inclusive?r.value<=t.value:r.value<t.value)||r.issues.push({origin:n,code:"too_big",maximum:typeof t.value=="object"?t.value.getTime():t.value,input:r.value,inclusive:t.inclusive,inst:e,continue:!t.abort})}}),$ZodCheckGreaterThan=$constructor("$ZodCheckGreaterThan",(e,t)=>{$ZodCheck.init(e,t);const n=numericOriginMap[typeof t.value];e._zod.onattach.push(r=>{const o=r._zod.bag,s=(t.inclusive?o.minimum:o.exclusiveMinimum)??Number.NEGATIVE_INFINITY;t.value>s&&(t.inclusive?o.minimum=t.value:o.exclusiveMinimum=t.value)}),e._zod.check=r=>{(t.inclusive?r.value>=t.value:r.value>t.value)||r.issues.push({origin:n,code:"too_small",minimum:typeof t.value=="object"?t.value.getTime():t.value,input:r.value,inclusive:t.inclusive,inst:e,continue:!t.abort})}}),$ZodCheckMultipleOf=$constructor("$ZodCheckMultipleOf",(e,t)=>{$ZodCheck.init(e,t),e._zod.onattach.push(n=>{var r;(r=n._zod.bag).multipleOf??(r.multipleOf=t.value)}),e._zod.check=n=>{if(typeof n.value!=typeof t.value)throw new Error("Cannot mix number and bigint in multiple_of check.");(typeof n.value=="bigint"?n.value%t.value===BigInt(0):floatSafeRemainder(n.value,t.value)===0)||n.issues.push({origin:typeof n.value,code:"not_multiple_of",divisor:t.value,input:n.value,inst:e,continue:!t.abort})}}),$ZodCheckNumberFormat=$constructor("$ZodCheckNumberFormat",(e,t)=>{var i;$ZodCheck.init(e,t),t.format=t.format||"float64";const n=(i=t.format)==null?void 0:i.includes("int"),r=n?"int":"number",[o,s]=NUMBER_FORMAT_RANGES[t.format];e._zod.onattach.push(a=>{const l=a._zod.bag;l.format=t.format,l.minimum=o,l.maximum=s,n&&(l.pattern=integer)}),e._zod.check=a=>{const l=a.value;if(n){if(!Number.isInteger(l)){a.issues.push({expected:r,format:t.format,code:"invalid_type",continue:!1,input:l,inst:e});return}if(!Number.isSafeInteger(l)){l>0?a.issues.push({input:l,code:"too_big",maximum:Number.MAX_SAFE_INTEGER,note:"Integers must be within the safe integer range.",inst:e,origin:r,inclusive:!0,continue:!t.abort}):a.issues.push({input:l,code:"too_small",minimum:Number.MIN_SAFE_INTEGER,note:"Integers must be within the safe integer range.",inst:e,origin:r,inclusive:!0,continue:!t.abort});return}}l<o&&a.issues.push({origin:"number",input:l,code:"too_small",minimum:o,inclusive:!0,inst:e,continue:!t.abort}),l>s&&a.issues.push({origin:"number",input:l,code:"too_big",maximum:s,inclusive:!0,inst:e,continue:!t.abort})}}),$ZodCheckMaxLength=$constructor("$ZodCheckMaxLength",(e,t)=>{var n;$ZodCheck.init(e,t),(n=e._zod.def).when??(n.when=r=>{const o=r.value;return!nullish(o)&&o.length!==void 0}),e._zod.onattach.push(r=>{const o=r._zod.bag.maximum??Number.POSITIVE_INFINITY;t.maximum<o&&(r._zod.bag.maximum=t.maximum)}),e._zod.check=r=>{const o=r.value;if(o.length<=t.maximum)return;const i=getLengthableOrigin(o);r.issues.push({origin:i,code:"too_big",maximum:t.maximum,inclusive:!0,input:o,inst:e,continue:!t.abort})}}),$ZodCheckMinLength=$constructor("$ZodCheckMinLength",(e,t)=>{var n;$ZodCheck.init(e,t),(n=e._zod.def).when??(n.when=r=>{const o=r.value;return!nullish(o)&&o.length!==void 0}),e._zod.onattach.push(r=>{const o=r._zod.bag.minimum??Number.NEGATIVE_INFINITY;t.minimum>o&&(r._zod.bag.minimum=t.minimum)}),e._zod.check=r=>{const o=r.value;if(o.length>=t.minimum)return;const i=getLengthableOrigin(o);r.issues.push({origin:i,code:"too_small",minimum:t.minimum,inclusive:!0,input:o,inst:e,continue:!t.abort})}}),$ZodCheckLengthEquals=$constructor("$ZodCheckLengthEquals",(e,t)=>{var n;$ZodCheck.init(e,t),(n=e._zod.def).when??(n.when=r=>{const o=r.value;return!nullish(o)&&o.length!==void 0}),e._zod.onattach.push(r=>{const o=r._zod.bag;o.minimum=t.length,o.maximum=t.length,o.length=t.length}),e._zod.check=r=>{const o=r.value,s=o.length;if(s===t.length)return;const i=getLengthableOrigin(o),a=s>t.length;r.issues.push({origin:i,...a?{code:"too_big",maximum:t.length}:{code:"too_small",minimum:t.length},inclusive:!0,exact:!0,input:r.value,inst:e,continue:!t.abort})}}),$ZodCheckStringFormat=$constructor("$ZodCheckStringFormat",(e,t)=>{var n,r;$ZodCheck.init(e,t),e._zod.onattach.push(o=>{const s=o._zod.bag;s.format=t.format,t.pattern&&(s.patterns??(s.patterns=new Set),s.patterns.add(t.pattern))}),t.pattern?(n=e._zod).check??(n.check=o=>{t.pattern.lastIndex=0,!t.pattern.test(o.value)&&o.issues.push({origin:"string",code:"invalid_format",format:t.format,input:o.value,...t.pattern?{pattern:t.pattern.toString()}:{},inst:e,continue:!t.abort})}):(r=e._zod).check??(r.check=()=>{})}),$ZodCheckRegex=$constructor("$ZodCheckRegex",(e,t)=>{$ZodCheckStringFormat.init(e,t),e._zod.check=n=>{t.pattern.lastIndex=0,!t.pattern.test(n.value)&&n.issues.push({origin:"string",code:"invalid_format",format:"regex",input:n.value,pattern:t.pattern.toString(),inst:e,continue:!t.abort})}}),$ZodCheckLowerCase=$constructor("$ZodCheckLowerCase",(e,t)=>{t.pattern??(t.pattern=lowercase),$ZodCheckStringFormat.init(e,t)}),$ZodCheckUpperCase=$constructor("$ZodCheckUpperCase",(e,t)=>{t.pattern??(t.pattern=uppercase),$ZodCheckStringFormat.init(e,t)}),$ZodCheckIncludes=$constructor("$ZodCheckIncludes",(e,t)=>{$ZodCheck.init(e,t);const n=escapeRegex(t.includes),r=new RegExp(typeof t.position=="number"?`^.{${t.position}}${n}`:n);t.pattern=r,e._zod.onattach.push(o=>{const s=o._zod.bag;s.patterns??(s.patterns=new Set),s.patterns.add(r)}),e._zod.check=o=>{o.value.includes(t.includes,t.position)||o.issues.push({origin:"string",code:"invalid_format",format:"includes",includes:t.includes,input:o.value,inst:e,continue:!t.abort})}}),$ZodCheckStartsWith=$constructor("$ZodCheckStartsWith",(e,t)=>{$ZodCheck.init(e,t);const n=new RegExp(`^${escapeRegex(t.prefix)}.*`);t.pattern??(t.pattern=n),e._zod.onattach.push(r=>{const o=r._zod.bag;o.patterns??(o.patterns=new Set),o.patterns.add(n)}),e._zod.check=r=>{r.value.startsWith(t.prefix)||r.issues.push({origin:"string",code:"invalid_format",format:"starts_with",prefix:t.prefix,input:r.value,inst:e,continue:!t.abort})}}),$ZodCheckEndsWith=$constructor("$ZodCheckEndsWith",(e,t)=>{$ZodCheck.init(e,t);const n=new RegExp(`.*${escapeRegex(t.suffix)}$`);t.pattern??(t.pattern=n),e._zod.onattach.push(r=>{const o=r._zod.bag;o.patterns??(o.patterns=new Set),o.patterns.add(n)}),e._zod.check=r=>{r.value.endsWith(t.suffix)||r.issues.push({origin:"string",code:"invalid_format",format:"ends_with",suffix:t.suffix,input:r.value,inst:e,continue:!t.abort})}}),$ZodCheckOverwrite=$constructor("$ZodCheckOverwrite",(e,t)=>{$ZodCheck.init(e,t),e._zod.check=n=>{n.value=t.tx(n.value)}});class Doc{constructor(t=[]){this.content=[],this.indent=0,this&&(this.args=t)}indented(t){this.indent+=1,t(this),this.indent-=1}write(t){if(typeof t=="function"){t(this,{execution:"sync"}),t(this,{execution:"async"});return}const r=t.split(`
`).filter(i=>i),o=Math.min(...r.map(i=>i.length-i.trimStart().length)),s=r.map(i=>i.slice(o)).map(i=>" ".repeat(this.indent*2)+i);for(const i of s)this.content.push(i)}compile(){const t=Function,n=this==null?void 0:this.args,o=[...((this==null?void 0:this.content)??[""]).map(s=>`  ${s}`)];return new t(...n,o.join(`
`))}}const version={major:4,minor:4,patch:3},$ZodType=$constructor("$ZodType",(e,t)=>{var o;var n;e??(e={}),e._zod.def=t,e._zod.bag=e._zod.bag||{},e._zod.version=version;const r=[...e._zod.def.checks??[]];e._zod.traits.has("$ZodCheck")&&r.unshift(e);for(const s of r)for(const i of s._zod.onattach)i(e);if(r.length===0)(n=e._zod).deferred??(n.deferred=[]),(o=e._zod.deferred)==null||o.push(()=>{e._zod.run=e._zod.parse});else{const s=(a,l,d)=>{let u=aborted(a),g;for(const h of l){if(h._zod.def.when){if(explicitlyAborted(a)||!h._zod.def.when(a))continue}else if(u)continue;const m=a.issues.length,y=h._zod.check(a);if(y instanceof Promise&&(d==null?void 0:d.async)===!1)throw new $ZodAsyncError;if(g||y instanceof Promise)g=(g??Promise.resolve()).then(async()=>{await y,a.issues.length!==m&&(u||(u=aborted(a,m)))});else{if(a.issues.length===m)continue;u||(u=aborted(a,m))}}return g?g.then(()=>a):a},i=(a,l,d)=>{if(aborted(a))return a.aborted=!0,a;const u=s(l,r,d);if(u instanceof Promise){if(d.async===!1)throw new $ZodAsyncError;return u.then(g=>e._zod.parse(g,d))}return e._zod.parse(u,d)};e._zod.run=(a,l)=>{if(l.skipChecks)return e._zod.parse(a,l);if(l.direction==="backward"){const u=e._zod.parse({value:a.value,issues:[]},{...l,skipChecks:!0});return u instanceof Promise?u.then(g=>i(g,a,l)):i(u,a,l)}const d=e._zod.parse(a,l);if(d instanceof Promise){if(l.async===!1)throw new $ZodAsyncError;return d.then(u=>s(u,r,l))}return s(d,r,l)}}defineLazy(e,"~standard",()=>({validate:s=>{var i;try{const a=safeParse$1(e,s);return a.success?{value:a.data}:{issues:(i=a.error)==null?void 0:i.issues}}catch{return safeParseAsync$1(e,s).then(l=>{var d;return l.success?{value:l.data}:{issues:(d=l.error)==null?void 0:d.issues}})}},vendor:"zod",version:1}))}),$ZodString=$constructor("$ZodString",(e,t)=>{var n;$ZodType.init(e,t),e._zod.pattern=[...((n=e==null?void 0:e._zod.bag)==null?void 0:n.patterns)??[]].pop()??string$1(e._zod.bag),e._zod.parse=(r,o)=>{if(t.coerce)try{r.value=String(r.value)}catch{}return typeof r.value=="string"||r.issues.push({expected:"string",code:"invalid_type",input:r.value,inst:e}),r}}),$ZodStringFormat=$constructor("$ZodStringFormat",(e,t)=>{$ZodCheckStringFormat.init(e,t),$ZodString.init(e,t)}),$ZodGUID=$constructor("$ZodGUID",(e,t)=>{t.pattern??(t.pattern=guid),$ZodStringFormat.init(e,t)}),$ZodUUID=$constructor("$ZodUUID",(e,t)=>{if(t.version){const r={v1:1,v2:2,v3:3,v4:4,v5:5,v6:6,v7:7,v8:8}[t.version];if(r===void 0)throw new Error(`Invalid UUID version: "${t.version}"`);t.pattern??(t.pattern=uuid(r))}else t.pattern??(t.pattern=uuid());$ZodStringFormat.init(e,t)}),$ZodEmail=$constructor("$ZodEmail",(e,t)=>{t.pattern??(t.pattern=email),$ZodStringFormat.init(e,t)}),$ZodURL=$constructor("$ZodURL",(e,t)=>{$ZodStringFormat.init(e,t),e._zod.check=n=>{var r;try{const o=n.value.trim();if(!t.normalize&&((r=t.protocol)==null?void 0:r.source)===httpProtocol.source&&!/^https?:\/\//i.test(o)){n.issues.push({code:"invalid_format",format:"url",note:"Invalid URL format",input:n.value,inst:e,continue:!t.abort});return}const s=new URL(o);t.hostname&&(t.hostname.lastIndex=0,t.hostname.test(s.hostname)||n.issues.push({code:"invalid_format",format:"url",note:"Invalid hostname",pattern:t.hostname.source,input:n.value,inst:e,continue:!t.abort})),t.protocol&&(t.protocol.lastIndex=0,t.protocol.test(s.protocol.endsWith(":")?s.protocol.slice(0,-1):s.protocol)||n.issues.push({code:"invalid_format",format:"url",note:"Invalid protocol",pattern:t.protocol.source,input:n.value,inst:e,continue:!t.abort})),t.normalize?n.value=s.href:n.value=o;return}catch{n.issues.push({code:"invalid_format",format:"url",input:n.value,inst:e,continue:!t.abort})}}}),$ZodEmoji=$constructor("$ZodEmoji",(e,t)=>{t.pattern??(t.pattern=emoji()),$ZodStringFormat.init(e,t)}),$ZodNanoID=$constructor("$ZodNanoID",(e,t)=>{t.pattern??(t.pattern=nanoid),$ZodStringFormat.init(e,t)}),$ZodCUID=$constructor("$ZodCUID",(e,t)=>{t.pattern??(t.pattern=cuid),$ZodStringFormat.init(e,t)}),$ZodCUID2=$constructor("$ZodCUID2",(e,t)=>{t.pattern??(t.pattern=cuid2),$ZodStringFormat.init(e,t)}),$ZodULID=$constructor("$ZodULID",(e,t)=>{t.pattern??(t.pattern=ulid),$ZodStringFormat.init(e,t)}),$ZodXID=$constructor("$ZodXID",(e,t)=>{t.pattern??(t.pattern=xid),$ZodStringFormat.init(e,t)}),$ZodKSUID=$constructor("$ZodKSUID",(e,t)=>{t.pattern??(t.pattern=ksuid),$ZodStringFormat.init(e,t)}),$ZodISODateTime=$constructor("$ZodISODateTime",(e,t)=>{t.pattern??(t.pattern=datetime$1(t)),$ZodStringFormat.init(e,t)}),$ZodISODate=$constructor("$ZodISODate",(e,t)=>{t.pattern??(t.pattern=date$1),$ZodStringFormat.init(e,t)}),$ZodISOTime=$constructor("$ZodISOTime",(e,t)=>{t.pattern??(t.pattern=time$1(t)),$ZodStringFormat.init(e,t)}),$ZodISODuration=$constructor("$ZodISODuration",(e,t)=>{t.pattern??(t.pattern=duration$1),$ZodStringFormat.init(e,t)}),$ZodIPv4=$constructor("$ZodIPv4",(e,t)=>{t.pattern??(t.pattern=ipv4),$ZodStringFormat.init(e,t),e._zod.bag.format="ipv4"}),$ZodIPv6=$constructor("$ZodIPv6",(e,t)=>{t.pattern??(t.pattern=ipv6),$ZodStringFormat.init(e,t),e._zod.bag.format="ipv6",e._zod.check=n=>{try{new URL(`http://[${n.value}]`)}catch{n.issues.push({code:"invalid_format",format:"ipv6",input:n.value,inst:e,continue:!t.abort})}}}),$ZodCIDRv4=$constructor("$ZodCIDRv4",(e,t)=>{t.pattern??(t.pattern=cidrv4),$ZodStringFormat.init(e,t)}),$ZodCIDRv6=$constructor("$ZodCIDRv6",(e,t)=>{t.pattern??(t.pattern=cidrv6),$ZodStringFormat.init(e,t),e._zod.check=n=>{const r=n.value.split("/");try{if(r.length!==2)throw new Error;const[o,s]=r;if(!s)throw new Error;const i=Number(s);if(`${i}`!==s)throw new Error;if(i<0||i>128)throw new Error;new URL(`http://[${o}]`)}catch{n.issues.push({code:"invalid_format",format:"cidrv6",input:n.value,inst:e,continue:!t.abort})}}});function isValidBase64(e){if(e==="")return!0;if(/\s/.test(e)||e.length%4!==0)return!1;try{return atob(e),!0}catch{return!1}}const $ZodBase64=$constructor("$ZodBase64",(e,t)=>{t.pattern??(t.pattern=base64),$ZodStringFormat.init(e,t),e._zod.bag.contentEncoding="base64",e._zod.check=n=>{isValidBase64(n.value)||n.issues.push({code:"invalid_format",format:"base64",input:n.value,inst:e,continue:!t.abort})}});function isValidBase64URL(e){if(!base64url.test(e))return!1;const t=e.replace(/[-_]/g,r=>r==="-"?"+":"/"),n=t.padEnd(Math.ceil(t.length/4)*4,"=");return isValidBase64(n)}const $ZodBase64URL=$constructor("$ZodBase64URL",(e,t)=>{t.pattern??(t.pattern=base64url),$ZodStringFormat.init(e,t),e._zod.bag.contentEncoding="base64url",e._zod.check=n=>{isValidBase64URL(n.value)||n.issues.push({code:"invalid_format",format:"base64url",input:n.value,inst:e,continue:!t.abort})}}),$ZodE164=$constructor("$ZodE164",(e,t)=>{t.pattern??(t.pattern=e164),$ZodStringFormat.init(e,t)});function isValidJWT(e,t=null){try{const n=e.split(".");if(n.length!==3)return!1;const[r]=n;if(!r)return!1;const o=JSON.parse(atob(r));return!("typ"in o&&(o==null?void 0:o.typ)!=="JWT"||!o.alg||t&&(!("alg"in o)||o.alg!==t))}catch{return!1}}const $ZodJWT=$constructor("$ZodJWT",(e,t)=>{$ZodStringFormat.init(e,t),e._zod.check=n=>{isValidJWT(n.value,t.alg)||n.issues.push({code:"invalid_format",format:"jwt",input:n.value,inst:e,continue:!t.abort})}}),$ZodNumber=$constructor("$ZodNumber",(e,t)=>{$ZodType.init(e,t),e._zod.pattern=e._zod.bag.pattern??number$1,e._zod.parse=(n,r)=>{if(t.coerce)try{n.value=Number(n.value)}catch{}const o=n.value;if(typeof o=="number"&&!Number.isNaN(o)&&Number.isFinite(o))return n;const s=typeof o=="number"?Number.isNaN(o)?"NaN":Number.isFinite(o)?void 0:"Infinity":void 0;return n.issues.push({expected:"number",code:"invalid_type",input:o,inst:e,...s?{received:s}:{}}),n}}),$ZodNumberFormat=$constructor("$ZodNumberFormat",(e,t)=>{$ZodCheckNumberFormat.init(e,t),$ZodNumber.init(e,t)}),$ZodBoolean=$constructor("$ZodBoolean",(e,t)=>{$ZodType.init(e,t),e._zod.pattern=boolean$1,e._zod.parse=(n,r)=>{if(t.coerce)try{n.value=!!n.value}catch{}const o=n.value;return typeof o=="boolean"||n.issues.push({expected:"boolean",code:"invalid_type",input:o,inst:e}),n}}),$ZodUnknown=$constructor("$ZodUnknown",(e,t)=>{$ZodType.init(e,t),e._zod.parse=n=>n}),$ZodNever=$constructor("$ZodNever",(e,t)=>{$ZodType.init(e,t),e._zod.parse=(n,r)=>(n.issues.push({expected:"never",code:"invalid_type",input:n.value,inst:e}),n)});function handleArrayResult(e,t,n){e.issues.length&&t.issues.push(...prefixIssues(n,e.issues)),t.value[n]=e.value}const $ZodArray=$constructor("$ZodArray",(e,t)=>{$ZodType.init(e,t),e._zod.parse=(n,r)=>{const o=n.value;if(!Array.isArray(o))return n.issues.push({expected:"array",code:"invalid_type",input:o,inst:e}),n;n.value=Array(o.length);const s=[];for(let i=0;i<o.length;i++){const a=o[i],l=t.element._zod.run({value:a,issues:[]},r);l instanceof Promise?s.push(l.then(d=>handleArrayResult(d,n,i))):handleArrayResult(l,n,i)}return s.length?Promise.all(s).then(()=>n):n}});function handlePropertyResult(e,t,n,r,o,s){const i=n in r;if(e.issues.length){if(o&&s&&!i)return;t.issues.push(...prefixIssues(n,e.issues))}if(!i&&!o){e.issues.length||t.issues.push({code:"invalid_type",expected:"nonoptional",input:void 0,path:[n]});return}e.value===void 0?i&&(t.value[n]=void 0):t.value[n]=e.value}function normalizeDef(e){var r,o,s,i;const t=Object.keys(e.shape);for(const a of t)if(!((i=(s=(o=(r=e.shape)==null?void 0:r[a])==null?void 0:o._zod)==null?void 0:s.traits)!=null&&i.has("$ZodType")))throw new Error(`Invalid element at key "${a}": expected a Zod schema`);const n=optionalKeys(e.shape);return{...e,keys:t,keySet:new Set(t),numKeys:t.length,optionalKeys:new Set(n)}}function handleCatchall(e,t,n,r,o,s){const i=[],a=o.keySet,l=o.catchall._zod,d=l.def.type,u=l.optin==="optional",g=l.optout==="optional";for(const h in t){if(h==="__proto__"||a.has(h))continue;if(d==="never"){i.push(h);continue}const m=l.run({value:t[h],issues:[]},r);m instanceof Promise?e.push(m.then(y=>handlePropertyResult(y,n,h,t,u,g))):handlePropertyResult(m,n,h,t,u,g)}return i.length&&n.issues.push({code:"unrecognized_keys",keys:i,input:t,inst:s}),e.length?Promise.all(e).then(()=>n):n}const $ZodObject=$constructor("$ZodObject",(e,t)=>{$ZodType.init(e,t);const n=Object.getOwnPropertyDescriptor(t,"shape");if(!(n!=null&&n.get)){const a=t.shape;Object.defineProperty(t,"shape",{get:()=>{const l={...a};return Object.defineProperty(t,"shape",{value:l}),l}})}const r=cached(()=>normalizeDef(t));defineLazy(e._zod,"propValues",()=>{const a=t.shape,l={};for(const d in a){const u=a[d]._zod;if(u.values){l[d]??(l[d]=new Set);for(const g of u.values)l[d].add(g)}}return l});const o=isObject,s=t.catchall;let i;e._zod.parse=(a,l)=>{i??(i=r.value);const d=a.value;if(!o(d))return a.issues.push({expected:"object",code:"invalid_type",input:d,inst:e}),a;a.value={};const u=[],g=i.shape;for(const h of i.keys){const m=g[h],y=m._zod.optin==="optional",v=m._zod.optout==="optional",T=m._zod.run({value:d[h],issues:[]},l);T instanceof Promise?u.push(T.then(k=>handlePropertyResult(k,a,h,d,y,v))):handlePropertyResult(T,a,h,d,y,v)}return s?handleCatchall(u,d,a,l,r.value,e):u.length?Promise.all(u).then(()=>a):a}}),$ZodObjectJIT=$constructor("$ZodObjectJIT",(e,t)=>{$ZodObject.init(e,t);const n=e._zod.parse,r=cached(()=>normalizeDef(t)),o=h=>{var D,U;const m=new Doc(["shape","payload","ctx"]),y=r.value,v=Y=>{const O=esc(Y);return`shape[${O}]._zod.run({ value: input[${O}], issues: [] }, ctx)`};m.write("const input = payload.value;");const T=Object.create(null);let k=0;for(const Y of y.keys)T[Y]=`key_${k++}`;m.write("const newResult = {};");for(const Y of y.keys){const O=T[Y],M=esc(Y),V=h[Y],X=((D=V==null?void 0:V._zod)==null?void 0:D.optin)==="optional",Q=((U=V==null?void 0:V._zod)==null?void 0:U.optout)==="optional";m.write(`const ${O} = ${v(Y)};`),X&&Q?m.write(`
        if (${O}.issues.length) {
          if (${M} in input) {
            payload.issues = payload.issues.concat(${O}.issues.map(iss => ({
              ...iss,
              path: iss.path ? [${M}, ...iss.path] : [${M}]
            })));
          }
        }
        
        if (${O}.value === undefined) {
          if (${M} in input) {
            newResult[${M}] = undefined;
          }
        } else {
          newResult[${M}] = ${O}.value;
        }
        
      `):X?m.write(`
        if (${O}.issues.length) {
          payload.issues = payload.issues.concat(${O}.issues.map(iss => ({
            ...iss,
            path: iss.path ? [${M}, ...iss.path] : [${M}]
          })));
        }
        
        if (${O}.value === undefined) {
          if (${M} in input) {
            newResult[${M}] = undefined;
          }
        } else {
          newResult[${M}] = ${O}.value;
        }
        
      `):m.write(`
        const ${O}_present = ${M} in input;
        if (${O}.issues.length) {
          payload.issues = payload.issues.concat(${O}.issues.map(iss => ({
            ...iss,
            path: iss.path ? [${M}, ...iss.path] : [${M}]
          })));
        }
        if (!${O}_present && !${O}.issues.length) {
          payload.issues.push({
            code: "invalid_type",
            expected: "nonoptional",
            input: undefined,
            path: [${M}]
          });
        }

        if (${O}_present) {
          if (${O}.value === undefined) {
            newResult[${M}] = undefined;
          } else {
            newResult[${M}] = ${O}.value;
          }
        }

      `)}m.write("payload.value = newResult;"),m.write("return payload;");const S=m.compile();return(Y,O)=>S(h,Y,O)};let s;const i=isObject,a=!globalConfig.jitless,d=a&&allowsEval.value,u=t.catchall;let g;e._zod.parse=(h,m)=>{g??(g=r.value);const y=h.value;return i(y)?a&&d&&(m==null?void 0:m.async)===!1&&m.jitless!==!0?(s||(s=o(t.shape)),h=s(h,m),u?handleCatchall([],y,h,m,g,e):h):n(h,m):(h.issues.push({expected:"object",code:"invalid_type",input:y,inst:e}),h)}});function handleUnionResults(e,t,n,r){for(const s of e)if(s.issues.length===0)return t.value=s.value,t;const o=e.filter(s=>!aborted(s));return o.length===1?(t.value=o[0].value,o[0]):(t.issues.push({code:"invalid_union",input:t.value,inst:n,errors:e.map(s=>s.issues.map(i=>finalizeIssue(i,r,config())))}),t)}const $ZodUnion=$constructor("$ZodUnion",(e,t)=>{$ZodType.init(e,t),defineLazy(e._zod,"optin",()=>t.options.some(r=>r._zod.optin==="optional")?"optional":void 0),defineLazy(e._zod,"optout",()=>t.options.some(r=>r._zod.optout==="optional")?"optional":void 0),defineLazy(e._zod,"values",()=>{if(t.options.every(r=>r._zod.values))return new Set(t.options.flatMap(r=>Array.from(r._zod.values)))}),defineLazy(e._zod,"pattern",()=>{if(t.options.every(r=>r._zod.pattern)){const r=t.options.map(o=>o._zod.pattern);return new RegExp(`^(${r.map(o=>cleanRegex(o.source)).join("|")})$`)}});const n=t.options.length===1?t.options[0]._zod.run:null;e._zod.parse=(r,o)=>{if(n)return n(r,o);let s=!1;const i=[];for(const a of t.options){const l=a._zod.run({value:r.value,issues:[]},o);if(l instanceof Promise)i.push(l),s=!0;else{if(l.issues.length===0)return l;i.push(l)}}return s?Promise.all(i).then(a=>handleUnionResults(a,r,e,o)):handleUnionResults(i,r,e,o)}}),$ZodIntersection=$constructor("$ZodIntersection",(e,t)=>{$ZodType.init(e,t),e._zod.parse=(n,r)=>{const o=n.value,s=t.left._zod.run({value:o,issues:[]},r),i=t.right._zod.run({value:o,issues:[]},r);return s instanceof Promise||i instanceof Promise?Promise.all([s,i]).then(([l,d])=>handleIntersectionResults(n,l,d)):handleIntersectionResults(n,s,i)}});function mergeValues(e,t){if(e===t)return{valid:!0,data:e};if(e instanceof Date&&t instanceof Date&&+e==+t)return{valid:!0,data:e};if(isPlainObject(e)&&isPlainObject(t)){const n=Object.keys(t),r=Object.keys(e).filter(s=>n.indexOf(s)!==-1),o={...e,...t};for(const s of r){const i=mergeValues(e[s],t[s]);if(!i.valid)return{valid:!1,mergeErrorPath:[s,...i.mergeErrorPath]};o[s]=i.data}return{valid:!0,data:o}}if(Array.isArray(e)&&Array.isArray(t)){if(e.length!==t.length)return{valid:!1,mergeErrorPath:[]};const n=[];for(let r=0;r<e.length;r++){const o=e[r],s=t[r],i=mergeValues(o,s);if(!i.valid)return{valid:!1,mergeErrorPath:[r,...i.mergeErrorPath]};n.push(i.data)}return{valid:!0,data:n}}return{valid:!1,mergeErrorPath:[]}}function handleIntersectionResults(e,t,n){const r=new Map;let o;for(const a of t.issues)if(a.code==="unrecognized_keys"){o??(o=a);for(const l of a.keys)r.has(l)||r.set(l,{}),r.get(l).l=!0}else e.issues.push(a);for(const a of n.issues)if(a.code==="unrecognized_keys")for(const l of a.keys)r.has(l)||r.set(l,{}),r.get(l).r=!0;else e.issues.push(a);const s=[...r].filter(([,a])=>a.l&&a.r).map(([a])=>a);if(s.length&&o&&e.issues.push({...o,keys:s}),aborted(e))return e;const i=mergeValues(t.value,n.value);if(!i.valid)throw new Error(`Unmergable intersection. Error path: ${JSON.stringify(i.mergeErrorPath)}`);return e.value=i.data,e}const $ZodEnum=$constructor("$ZodEnum",(e,t)=>{$ZodType.init(e,t);const n=getEnumValues(t.entries),r=new Set(n);e._zod.values=r,e._zod.pattern=new RegExp(`^(${n.filter(o=>propertyKeyTypes.has(typeof o)).map(o=>typeof o=="string"?escapeRegex(o):o.toString()).join("|")})$`),e._zod.parse=(o,s)=>{const i=o.value;return r.has(i)||o.issues.push({code:"invalid_value",values:n,input:i,inst:e}),o}}),$ZodTransform=$constructor("$ZodTransform",(e,t)=>{$ZodType.init(e,t),e._zod.optin="optional",e._zod.parse=(n,r)=>{if(r.direction==="backward")throw new $ZodEncodeError(e.constructor.name);const o=t.transform(n.value,n);if(r.async)return(o instanceof Promise?o:Promise.resolve(o)).then(i=>(n.value=i,n.fallback=!0,n));if(o instanceof Promise)throw new $ZodAsyncError;return n.value=o,n.fallback=!0,n}});function handleOptionalResult(e,t){return t===void 0&&(e.issues.length||e.fallback)?{issues:[],value:void 0}:e}const $ZodOptional=$constructor("$ZodOptional",(e,t)=>{$ZodType.init(e,t),e._zod.optin="optional",e._zod.optout="optional",defineLazy(e._zod,"values",()=>t.innerType._zod.values?new Set([...t.innerType._zod.values,void 0]):void 0),defineLazy(e._zod,"pattern",()=>{const n=t.innerType._zod.pattern;return n?new RegExp(`^(${cleanRegex(n.source)})?$`):void 0}),e._zod.parse=(n,r)=>{if(t.innerType._zod.optin==="optional"){const o=n.value,s=t.innerType._zod.run(n,r);return s instanceof Promise?s.then(i=>handleOptionalResult(i,o)):handleOptionalResult(s,o)}return n.value===void 0?n:t.innerType._zod.run(n,r)}}),$ZodExactOptional=$constructor("$ZodExactOptional",(e,t)=>{$ZodOptional.init(e,t),defineLazy(e._zod,"values",()=>t.innerType._zod.values),defineLazy(e._zod,"pattern",()=>t.innerType._zod.pattern),e._zod.parse=(n,r)=>t.innerType._zod.run(n,r)}),$ZodNullable=$constructor("$ZodNullable",(e,t)=>{$ZodType.init(e,t),defineLazy(e._zod,"optin",()=>t.innerType._zod.optin),defineLazy(e._zod,"optout",()=>t.innerType._zod.optout),defineLazy(e._zod,"pattern",()=>{const n=t.innerType._zod.pattern;return n?new RegExp(`^(${cleanRegex(n.source)}|null)$`):void 0}),defineLazy(e._zod,"values",()=>t.innerType._zod.values?new Set([...t.innerType._zod.values,null]):void 0),e._zod.parse=(n,r)=>n.value===null?n:t.innerType._zod.run(n,r)}),$ZodDefault=$constructor("$ZodDefault",(e,t)=>{$ZodType.init(e,t),e._zod.optin="optional",defineLazy(e._zod,"values",()=>t.innerType._zod.values),e._zod.parse=(n,r)=>{if(r.direction==="backward")return t.innerType._zod.run(n,r);if(n.value===void 0)return n.value=t.defaultValue,n;const o=t.innerType._zod.run(n,r);return o instanceof Promise?o.then(s=>handleDefaultResult(s,t)):handleDefaultResult(o,t)}});function handleDefaultResult(e,t){return e.value===void 0&&(e.value=t.defaultValue),e}const $ZodPrefault=$constructor("$ZodPrefault",(e,t)=>{$ZodType.init(e,t),e._zod.optin="optional",defineLazy(e._zod,"values",()=>t.innerType._zod.values),e._zod.parse=(n,r)=>(r.direction==="backward"||n.value===void 0&&(n.value=t.defaultValue),t.innerType._zod.run(n,r))}),$ZodNonOptional=$constructor("$ZodNonOptional",(e,t)=>{$ZodType.init(e,t),defineLazy(e._zod,"values",()=>{const n=t.innerType._zod.values;return n?new Set([...n].filter(r=>r!==void 0)):void 0}),e._zod.parse=(n,r)=>{const o=t.innerType._zod.run(n,r);return o instanceof Promise?o.then(s=>handleNonOptionalResult(s,e)):handleNonOptionalResult(o,e)}});function handleNonOptionalResult(e,t){return!e.issues.length&&e.value===void 0&&e.issues.push({code:"invalid_type",expected:"nonoptional",input:e.value,inst:t}),e}const $ZodCatch=$constructor("$ZodCatch",(e,t)=>{$ZodType.init(e,t),e._zod.optin="optional",defineLazy(e._zod,"optout",()=>t.innerType._zod.optout),defineLazy(e._zod,"values",()=>t.innerType._zod.values),e._zod.parse=(n,r)=>{if(r.direction==="backward")return t.innerType._zod.run(n,r);const o=t.innerType._zod.run(n,r);return o instanceof Promise?o.then(s=>(n.value=s.value,s.issues.length&&(n.value=t.catchValue({...n,error:{issues:s.issues.map(i=>finalizeIssue(i,r,config()))},input:n.value}),n.issues=[],n.fallback=!0),n)):(n.value=o.value,o.issues.length&&(n.value=t.catchValue({...n,error:{issues:o.issues.map(s=>finalizeIssue(s,r,config()))},input:n.value}),n.issues=[],n.fallback=!0),n)}}),$ZodPipe=$constructor("$ZodPipe",(e,t)=>{$ZodType.init(e,t),defineLazy(e._zod,"values",()=>t.in._zod.values),defineLazy(e._zod,"optin",()=>t.in._zod.optin),defineLazy(e._zod,"optout",()=>t.out._zod.optout),defineLazy(e._zod,"propValues",()=>t.in._zod.propValues),e._zod.parse=(n,r)=>{if(r.direction==="backward"){const s=t.out._zod.run(n,r);return s instanceof Promise?s.then(i=>handlePipeResult(i,t.in,r)):handlePipeResult(s,t.in,r)}const o=t.in._zod.run(n,r);return o instanceof Promise?o.then(s=>handlePipeResult(s,t.out,r)):handlePipeResult(o,t.out,r)}});function handlePipeResult(e,t,n){return e.issues.length?(e.aborted=!0,e):t._zod.run({value:e.value,issues:e.issues,fallback:e.fallback},n)}const $ZodReadonly=$constructor("$ZodReadonly",(e,t)=>{$ZodType.init(e,t),defineLazy(e._zod,"propValues",()=>t.innerType._zod.propValues),defineLazy(e._zod,"values",()=>t.innerType._zod.values),defineLazy(e._zod,"optin",()=>{var n,r;return(r=(n=t.innerType)==null?void 0:n._zod)==null?void 0:r.optin}),defineLazy(e._zod,"optout",()=>{var n,r;return(r=(n=t.innerType)==null?void 0:n._zod)==null?void 0:r.optout}),e._zod.parse=(n,r)=>{if(r.direction==="backward")return t.innerType._zod.run(n,r);const o=t.innerType._zod.run(n,r);return o instanceof Promise?o.then(handleReadonlyResult):handleReadonlyResult(o)}});function handleReadonlyResult(e){return e.value=Object.freeze(e.value),e}const $ZodCustom=$constructor("$ZodCustom",(e,t)=>{$ZodCheck.init(e,t),$ZodType.init(e,t),e._zod.parse=(n,r)=>n,e._zod.check=n=>{const r=n.value,o=t.fn(r);if(o instanceof Promise)return o.then(s=>handleRefineResult(s,n,r,e));handleRefineResult(o,n,r,e)}});function handleRefineResult(e,t,n,r){if(!e){const o={code:"custom",input:n,inst:r,path:[...r._zod.def.path??[]],continue:!r._zod.def.abort};r._zod.def.params&&(o.params=r._zod.def.params),t.issues.push(issue(o))}}var _a;class $ZodRegistry{constructor(){this._map=new WeakMap,this._idmap=new Map}add(t,...n){const r=n[0];return this._map.set(t,r),r&&typeof r=="object"&&"id"in r&&this._idmap.set(r.id,t),this}clear(){return this._map=new WeakMap,this._idmap=new Map,this}remove(t){const n=this._map.get(t);return n&&typeof n=="object"&&"id"in n&&this._idmap.delete(n.id),this._map.delete(t),this}get(t){const n=t._zod.parent;if(n){const r={...this.get(n)??{}};delete r.id;const o={...r,...this._map.get(t)};return Object.keys(o).length?o:void 0}return this._map.get(t)}has(t){return this._map.has(t)}}function registry(){return new $ZodRegistry}(_a=globalThis).__zod_globalRegistry??(_a.__zod_globalRegistry=registry());const globalRegistry=globalThis.__zod_globalRegistry;function _string(e,t){return new e({type:"string",...normalizeParams(t)})}function _email(e,t){return new e({type:"string",format:"email",check:"string_format",abort:!1,...normalizeParams(t)})}function _guid(e,t){return new e({type:"string",format:"guid",check:"string_format",abort:!1,...normalizeParams(t)})}function _uuid(e,t){return new e({type:"string",format:"uuid",check:"string_format",abort:!1,...normalizeParams(t)})}function _uuidv4(e,t){return new e({type:"string",format:"uuid",check:"string_format",abort:!1,version:"v4",...normalizeParams(t)})}function _uuidv6(e,t){return new e({type:"string",format:"uuid",check:"string_format",abort:!1,version:"v6",...normalizeParams(t)})}function _uuidv7(e,t){return new e({type:"string",format:"uuid",check:"string_format",abort:!1,version:"v7",...normalizeParams(t)})}function _url(e,t){return new e({type:"string",format:"url",check:"string_format",abort:!1,...normalizeParams(t)})}function _emoji(e,t){return new e({type:"string",format:"emoji",check:"string_format",abort:!1,...normalizeParams(t)})}function _nanoid(e,t){return new e({type:"string",format:"nanoid",check:"string_format",abort:!1,...normalizeParams(t)})}function _cuid(e,t){return new e({type:"string",format:"cuid",check:"string_format",abort:!1,...normalizeParams(t)})}function _cuid2(e,t){return new e({type:"string",format:"cuid2",check:"string_format",abort:!1,...normalizeParams(t)})}function _ulid(e,t){return new e({type:"string",format:"ulid",check:"string_format",abort:!1,...normalizeParams(t)})}function _xid(e,t){return new e({type:"string",format:"xid",check:"string_format",abort:!1,...normalizeParams(t)})}function _ksuid(e,t){return new e({type:"string",format:"ksuid",check:"string_format",abort:!1,...normalizeParams(t)})}function _ipv4(e,t){return new e({type:"string",format:"ipv4",check:"string_format",abort:!1,...normalizeParams(t)})}function _ipv6(e,t){return new e({type:"string",format:"ipv6",check:"string_format",abort:!1,...normalizeParams(t)})}function _cidrv4(e,t){return new e({type:"string",format:"cidrv4",check:"string_format",abort:!1,...normalizeParams(t)})}function _cidrv6(e,t){return new e({type:"string",format:"cidrv6",check:"string_format",abort:!1,...normalizeParams(t)})}function _base64(e,t){return new e({type:"string",format:"base64",check:"string_format",abort:!1,...normalizeParams(t)})}function _base64url(e,t){return new e({type:"string",format:"base64url",check:"string_format",abort:!1,...normalizeParams(t)})}function _e164(e,t){return new e({type:"string",format:"e164",check:"string_format",abort:!1,...normalizeParams(t)})}function _jwt(e,t){return new e({type:"string",format:"jwt",check:"string_format",abort:!1,...normalizeParams(t)})}function _isoDateTime(e,t){return new e({type:"string",format:"datetime",check:"string_format",offset:!1,local:!1,precision:null,...normalizeParams(t)})}function _isoDate(e,t){return new e({type:"string",format:"date",check:"string_format",...normalizeParams(t)})}function _isoTime(e,t){return new e({type:"string",format:"time",check:"string_format",precision:null,...normalizeParams(t)})}function _isoDuration(e,t){return new e({type:"string",format:"duration",check:"string_format",...normalizeParams(t)})}function _number(e,t){return new e({type:"number",checks:[],...normalizeParams(t)})}function _int(e,t){return new e({type:"number",check:"number_format",abort:!1,format:"safeint",...normalizeParams(t)})}function _boolean(e,t){return new e({type:"boolean",...normalizeParams(t)})}function _unknown(e){return new e({type:"unknown"})}function _never(e,t){return new e({type:"never",...normalizeParams(t)})}function _lt(e,t){return new $ZodCheckLessThan({check:"less_than",...normalizeParams(t),value:e,inclusive:!1})}function _lte(e,t){return new $ZodCheckLessThan({check:"less_than",...normalizeParams(t),value:e,inclusive:!0})}function _gt(e,t){return new $ZodCheckGreaterThan({check:"greater_than",...normalizeParams(t),value:e,inclusive:!1})}function _gte(e,t){return new $ZodCheckGreaterThan({check:"greater_than",...normalizeParams(t),value:e,inclusive:!0})}function _multipleOf(e,t){return new $ZodCheckMultipleOf({check:"multiple_of",...normalizeParams(t),value:e})}function _maxLength(e,t){return new $ZodCheckMaxLength({check:"max_length",...normalizeParams(t),maximum:e})}function _minLength(e,t){return new $ZodCheckMinLength({check:"min_length",...normalizeParams(t),minimum:e})}function _length(e,t){return new $ZodCheckLengthEquals({check:"length_equals",...normalizeParams(t),length:e})}function _regex(e,t){return new $ZodCheckRegex({check:"string_format",format:"regex",...normalizeParams(t),pattern:e})}function _lowercase(e){return new $ZodCheckLowerCase({check:"string_format",format:"lowercase",...normalizeParams(e)})}function _uppercase(e){return new $ZodCheckUpperCase({check:"string_format",format:"uppercase",...normalizeParams(e)})}function _includes(e,t){return new $ZodCheckIncludes({check:"string_format",format:"includes",...normalizeParams(t),includes:e})}function _startsWith(e,t){return new $ZodCheckStartsWith({check:"string_format",format:"starts_with",...normalizeParams(t),prefix:e})}function _endsWith(e,t){return new $ZodCheckEndsWith({check:"string_format",format:"ends_with",...normalizeParams(t),suffix:e})}function _overwrite(e){return new $ZodCheckOverwrite({check:"overwrite",tx:e})}function _normalize(e){return _overwrite(t=>t.normalize(e))}function _trim(){return _overwrite(e=>e.trim())}function _toLowerCase(){return _overwrite(e=>e.toLowerCase())}function _toUpperCase(){return _overwrite(e=>e.toUpperCase())}function _slugify(){return _overwrite(e=>slugify(e))}function _array(e,t,n){return new e({type:"array",element:t,...normalizeParams(n)})}function _refine(e,t,n){return new e({type:"custom",check:"custom",fn:t,...normalizeParams(n)})}function _superRefine(e,t){const n=_check(r=>(r.addIssue=o=>{if(typeof o=="string")r.issues.push(issue(o,r.value,n._zod.def));else{const s=o;s.fatal&&(s.continue=!1),s.code??(s.code="custom"),s.input??(s.input=r.value),s.inst??(s.inst=n),s.continue??(s.continue=!n._zod.def.abort),r.issues.push(issue(s))}},e(r.value,r)),t);return n}function _check(e,t){const n=new $ZodCheck({check:"custom",...normalizeParams(t)});return n._zod.check=e,n}function initializeContext(e){let t=(e==null?void 0:e.target)??"draft-2020-12";return t==="draft-4"&&(t="draft-04"),t==="draft-7"&&(t="draft-07"),{processors:e.processors??{},metadataRegistry:(e==null?void 0:e.metadata)??globalRegistry,target:t,unrepresentable:(e==null?void 0:e.unrepresentable)??"throw",override:(e==null?void 0:e.override)??(()=>{}),io:(e==null?void 0:e.io)??"output",counter:0,seen:new Map,cycles:(e==null?void 0:e.cycles)??"ref",reused:(e==null?void 0:e.reused)??"inline",external:(e==null?void 0:e.external)??void 0}}function process(e,t,n={path:[],schemaPath:[]}){var u,g;var r;const o=e._zod.def,s=t.seen.get(e);if(s)return s.count++,n.schemaPath.includes(e)&&(s.cycle=n.path),s.schema;const i={schema:{},count:1,cycle:void 0,path:n.path};t.seen.set(e,i);const a=(g=(u=e._zod).toJSONSchema)==null?void 0:g.call(u);if(a)i.schema=a;else{const h={...n,schemaPath:[...n.schemaPath,e],path:n.path};if(e._zod.processJSONSchema)e._zod.processJSONSchema(t,i.schema,h);else{const y=i.schema,v=t.processors[o.type];if(!v)throw new Error(`[toJSONSchema]: Non-representable type encountered: ${o.type}`);v(e,t,y,h)}const m=e._zod.parent;m&&(i.ref||(i.ref=m),process(m,t,h),t.seen.get(m).isParent=!0)}const l=t.metadataRegistry.get(e);return l&&Object.assign(i.schema,l),t.io==="input"&&isTransforming(e)&&(delete i.schema.examples,delete i.schema.default),t.io==="input"&&"_prefault"in i.schema&&((r=i.schema).default??(r.default=i.schema._prefault)),delete i.schema._prefault,t.seen.get(e).schema}function extractDefs(e,t){var i,a,l,d;const n=e.seen.get(t);if(!n)throw new Error("Unprocessed schema. This is a bug in Zod.");const r=new Map;for(const u of e.seen.entries()){const g=(i=e.metadataRegistry.get(u[0]))==null?void 0:i.id;if(g){const h=r.get(g);if(h&&h!==u[0])throw new Error(`Duplicate schema id "${g}" detected during JSON Schema conversion. Two different schemas cannot share the same id when converted together.`);r.set(g,u[0])}}const o=u=>{var v;const g=e.target==="draft-2020-12"?"$defs":"definitions";if(e.external){const T=(v=e.external.registry.get(u[0]))==null?void 0:v.id,k=e.external.uri??(D=>D);if(T)return{ref:k(T)};const S=u[1].defId??u[1].schema.id??`schema${e.counter++}`;return u[1].defId=S,{defId:S,ref:`${k("__shared")}#/${g}/${S}`}}if(u[1]===n)return{ref:"#"};const m=`#/${g}/`,y=u[1].schema.id??`__schema${e.counter++}`;return{defId:y,ref:m+y}},s=u=>{if(u[1].schema.$ref)return;const g=u[1],{ref:h,defId:m}=o(u);g.def={...g.schema},m&&(g.defId=m);const y=g.schema;for(const v in y)delete y[v];y.$ref=h};if(e.cycles==="throw")for(const u of e.seen.entries()){const g=u[1];if(g.cycle)throw new Error(`Cycle detected: #/${(a=g.cycle)==null?void 0:a.join("/")}/<root>

Set the \`cycles\` parameter to \`"ref"\` to resolve cyclical schemas with defs.`)}for(const u of e.seen.entries()){const g=u[1];if(t===u[0]){s(u);continue}if(e.external){const m=(l=e.external.registry.get(u[0]))==null?void 0:l.id;if(t!==u[0]&&m){s(u);continue}}if((d=e.metadataRegistry.get(u[0]))==null?void 0:d.id){s(u);continue}if(g.cycle){s(u);continue}if(g.count>1&&e.reused==="ref"){s(u);continue}}}function finalize(e,t){var a,l,d,u;const n=e.seen.get(t);if(!n)throw new Error("Unprocessed schema. This is a bug in Zod.");const r=g=>{const h=e.seen.get(g);if(h.ref===null)return;const m=h.def??h.schema,y={...m},v=h.ref;if(h.ref=null,v){r(v);const k=e.seen.get(v),S=k.schema;if(S.$ref&&(e.target==="draft-07"||e.target==="draft-04"||e.target==="openapi-3.0")?(m.allOf=m.allOf??[],m.allOf.push(S)):Object.assign(m,S),Object.assign(m,y),g._zod.parent===v)for(const U in m)U==="$ref"||U==="allOf"||U in y||delete m[U];if(S.$ref&&k.def)for(const U in m)U==="$ref"||U==="allOf"||U in k.def&&JSON.stringify(m[U])===JSON.stringify(k.def[U])&&delete m[U]}const T=g._zod.parent;if(T&&T!==v){r(T);const k=e.seen.get(T);if(k!=null&&k.schema.$ref&&(m.$ref=k.schema.$ref,k.def))for(const S in m)S==="$ref"||S==="allOf"||S in k.def&&JSON.stringify(m[S])===JSON.stringify(k.def[S])&&delete m[S]}e.override({zodSchema:g,jsonSchema:m,path:h.path??[]})};for(const g of[...e.seen.entries()].reverse())r(g[0]);const o={};if(e.target==="draft-2020-12"?o.$schema="https://json-schema.org/draft/2020-12/schema":e.target==="draft-07"?o.$schema="http://json-schema.org/draft-07/schema#":e.target==="draft-04"?o.$schema="http://json-schema.org/draft-04/schema#":e.target,(a=e.external)!=null&&a.uri){const g=(l=e.external.registry.get(t))==null?void 0:l.id;if(!g)throw new Error("Schema is missing an `id` property");o.$id=e.external.uri(g)}Object.assign(o,n.def??n.schema);const s=(d=e.metadataRegistry.get(t))==null?void 0:d.id;s!==void 0&&o.id===s&&delete o.id;const i=((u=e.external)==null?void 0:u.defs)??{};for(const g of e.seen.entries()){const h=g[1];h.def&&h.defId&&(h.def.id===h.defId&&delete h.def.id,i[h.defId]=h.def)}e.external||Object.keys(i).length>0&&(e.target==="draft-2020-12"?o.$defs=i:o.definitions=i);try{const g=JSON.parse(JSON.stringify(o));return Object.defineProperty(g,"~standard",{value:{...t["~standard"],jsonSchema:{input:createStandardJSONSchemaMethod(t,"input",e.processors),output:createStandardJSONSchemaMethod(t,"output",e.processors)}},enumerable:!1,writable:!1}),g}catch{throw new Error("Error converting schema to JSON.")}}function isTransforming(e,t){const n=t??{seen:new Set};if(n.seen.has(e))return!1;n.seen.add(e);const r=e._zod.def;if(r.type==="transform")return!0;if(r.type==="array")return isTransforming(r.element,n);if(r.type==="set")return isTransforming(r.valueType,n);if(r.type==="lazy")return isTransforming(r.getter(),n);if(r.type==="promise"||r.type==="optional"||r.type==="nonoptional"||r.type==="nullable"||r.type==="readonly"||r.type==="default"||r.type==="prefault")return isTransforming(r.innerType,n);if(r.type==="intersection")return isTransforming(r.left,n)||isTransforming(r.right,n);if(r.type==="record"||r.type==="map")return isTransforming(r.keyType,n)||isTransforming(r.valueType,n);if(r.type==="pipe")return e._zod.traits.has("$ZodCodec")?!0:isTransforming(r.in,n)||isTransforming(r.out,n);if(r.type==="object"){for(const o in r.shape)if(isTransforming(r.shape[o],n))return!0;return!1}if(r.type==="union"){for(const o of r.options)if(isTransforming(o,n))return!0;return!1}if(r.type==="tuple"){for(const o of r.items)if(isTransforming(o,n))return!0;return!!(r.rest&&isTransforming(r.rest,n))}return!1}const createToJSONSchemaMethod=(e,t={})=>n=>{const r=initializeContext({...n,processors:t});return process(e,r),extractDefs(r,e),finalize(r,e)},createStandardJSONSchemaMethod=(e,t,n={})=>r=>{const{libraryOptions:o,target:s}=r??{},i=initializeContext({...o??{},target:s,io:t,processors:n});return process(e,i),extractDefs(i,e),finalize(i,e)},formatMap={guid:"uuid",url:"uri",datetime:"date-time",json_string:"json-string",regex:""},stringProcessor=(e,t,n,r)=>{const o=n;o.type="string";const{minimum:s,maximum:i,format:a,patterns:l,contentEncoding:d}=e._zod.bag;if(typeof s=="number"&&(o.minLength=s),typeof i=="number"&&(o.maxLength=i),a&&(o.format=formatMap[a]??a,o.format===""&&delete o.format,a==="time"&&delete o.format),d&&(o.contentEncoding=d),l&&l.size>0){const u=[...l];u.length===1?o.pattern=u[0].source:u.length>1&&(o.allOf=[...u.map(g=>({...t.target==="draft-07"||t.target==="draft-04"||t.target==="openapi-3.0"?{type:"string"}:{},pattern:g.source}))])}},numberProcessor=(e,t,n,r)=>{const o=n,{minimum:s,maximum:i,format:a,multipleOf:l,exclusiveMaximum:d,exclusiveMinimum:u}=e._zod.bag;typeof a=="string"&&a.includes("int")?o.type="integer":o.type="number";const g=typeof u=="number"&&u>=(s??Number.NEGATIVE_INFINITY),h=typeof d=="number"&&d<=(i??Number.POSITIVE_INFINITY),m=t.target==="draft-04"||t.target==="openapi-3.0";g?m?(o.minimum=u,o.exclusiveMinimum=!0):o.exclusiveMinimum=u:typeof s=="number"&&(o.minimum=s),h?m?(o.maximum=d,o.exclusiveMaximum=!0):o.exclusiveMaximum=d:typeof i=="number"&&(o.maximum=i),typeof l=="number"&&(o.multipleOf=l)},booleanProcessor=(e,t,n,r)=>{n.type="boolean"},bigintProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("BigInt cannot be represented in JSON Schema")},symbolProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Symbols cannot be represented in JSON Schema")},nullProcessor=(e,t,n,r)=>{t.target==="openapi-3.0"?(n.type="string",n.nullable=!0,n.enum=[null]):n.type="null"},undefinedProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Undefined cannot be represented in JSON Schema")},voidProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Void cannot be represented in JSON Schema")},neverProcessor=(e,t,n,r)=>{n.not={}},anyProcessor=(e,t,n,r)=>{},unknownProcessor=(e,t,n,r)=>{},dateProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Date cannot be represented in JSON Schema")},enumProcessor=(e,t,n,r)=>{const o=e._zod.def,s=getEnumValues(o.entries);s.every(i=>typeof i=="number")&&(n.type="number"),s.every(i=>typeof i=="string")&&(n.type="string"),n.enum=s},literalProcessor=(e,t,n,r)=>{const o=e._zod.def,s=[];for(const i of o.values)if(i===void 0){if(t.unrepresentable==="throw")throw new Error("Literal `undefined` cannot be represented in JSON Schema")}else if(typeof i=="bigint"){if(t.unrepresentable==="throw")throw new Error("BigInt literals cannot be represented in JSON Schema");s.push(Number(i))}else s.push(i);if(s.length!==0)if(s.length===1){const i=s[0];n.type=i===null?"null":typeof i,t.target==="draft-04"||t.target==="openapi-3.0"?n.enum=[i]:n.const=i}else s.every(i=>typeof i=="number")&&(n.type="number"),s.every(i=>typeof i=="string")&&(n.type="string"),s.every(i=>typeof i=="boolean")&&(n.type="boolean"),s.every(i=>i===null)&&(n.type="null"),n.enum=s},nanProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("NaN cannot be represented in JSON Schema")},templateLiteralProcessor=(e,t,n,r)=>{const o=n,s=e._zod.pattern;if(!s)throw new Error("Pattern not found in template literal");o.type="string",o.pattern=s.source},fileProcessor=(e,t,n,r)=>{const o=n,s={type:"string",format:"binary",contentEncoding:"binary"},{minimum:i,maximum:a,mime:l}=e._zod.bag;i!==void 0&&(s.minLength=i),a!==void 0&&(s.maxLength=a),l?l.length===1?(s.contentMediaType=l[0],Object.assign(o,s)):(Object.assign(o,s),o.anyOf=l.map(d=>({contentMediaType:d}))):Object.assign(o,s)},successProcessor=(e,t,n,r)=>{n.type="boolean"},customProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Custom types cannot be represented in JSON Schema")},functionProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Function types cannot be represented in JSON Schema")},transformProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Transforms cannot be represented in JSON Schema")},mapProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Map cannot be represented in JSON Schema")},setProcessor=(e,t,n,r)=>{if(t.unrepresentable==="throw")throw new Error("Set cannot be represented in JSON Schema")},arrayProcessor=(e,t,n,r)=>{const o=n,s=e._zod.def,{minimum:i,maximum:a}=e._zod.bag;typeof i=="number"&&(o.minItems=i),typeof a=="number"&&(o.maxItems=a),o.type="array",o.items=process(s.element,t,{...r,path:[...r.path,"items"]})},objectProcessor=(e,t,n,r)=>{var d;const o=n,s=e._zod.def;o.type="object",o.properties={};const i=s.shape;for(const u in i)o.properties[u]=process(i[u],t,{...r,path:[...r.path,"properties",u]});const a=new Set(Object.keys(i)),l=new Set([...a].filter(u=>{const g=s.shape[u]._zod;return t.io==="input"?g.optin===void 0:g.optout===void 0}));l.size>0&&(o.required=Array.from(l)),((d=s.catchall)==null?void 0:d._zod.def.type)==="never"?o.additionalProperties=!1:s.catchall?s.catchall&&(o.additionalProperties=process(s.catchall,t,{...r,path:[...r.path,"additionalProperties"]})):t.io==="output"&&(o.additionalProperties=!1)},unionProcessor=(e,t,n,r)=>{const o=e._zod.def,s=o.inclusive===!1,i=o.options.map((a,l)=>process(a,t,{...r,path:[...r.path,s?"oneOf":"anyOf",l]}));s?n.oneOf=i:n.anyOf=i},intersectionProcessor=(e,t,n,r)=>{const o=e._zod.def,s=process(o.left,t,{...r,path:[...r.path,"allOf",0]}),i=process(o.right,t,{...r,path:[...r.path,"allOf",1]}),a=d=>"allOf"in d&&Object.keys(d).length===1,l=[...a(s)?s.allOf:[s],...a(i)?i.allOf:[i]];n.allOf=l},tupleProcessor=(e,t,n,r)=>{const o=n,s=e._zod.def;o.type="array";const i=t.target==="draft-2020-12"?"prefixItems":"items",a=t.target==="draft-2020-12"||t.target==="openapi-3.0"?"items":"additionalItems",l=s.items.map((h,m)=>process(h,t,{...r,path:[...r.path,i,m]})),d=s.rest?process(s.rest,t,{...r,path:[...r.path,a,...t.target==="openapi-3.0"?[s.items.length]:[]]}):null;t.target==="draft-2020-12"?(o.prefixItems=l,d&&(o.items=d)):t.target==="openapi-3.0"?(o.items={anyOf:l},d&&o.items.anyOf.push(d),o.minItems=l.length,d||(o.maxItems=l.length)):(o.items=l,d&&(o.additionalItems=d));const{minimum:u,maximum:g}=e._zod.bag;typeof u=="number"&&(o.minItems=u),typeof g=="number"&&(o.maxItems=g)},recordProcessor=(e,t,n,r)=>{const o=n,s=e._zod.def;o.type="object";const i=s.keyType,a=i._zod.bag,l=a==null?void 0:a.patterns;if(s.mode==="loose"&&l&&l.size>0){const u=process(s.valueType,t,{...r,path:[...r.path,"patternProperties","*"]});o.patternProperties={};for(const g of l)o.patternProperties[g.source]=u}else(t.target==="draft-07"||t.target==="draft-2020-12")&&(o.propertyNames=process(s.keyType,t,{...r,path:[...r.path,"propertyNames"]})),o.additionalProperties=process(s.valueType,t,{...r,path:[...r.path,"additionalProperties"]});const d=i._zod.values;if(d){const u=[...d].filter(g=>typeof g=="string"||typeof g=="number");u.length>0&&(o.required=u)}},nullableProcessor=(e,t,n,r)=>{const o=e._zod.def,s=process(o.innerType,t,r),i=t.seen.get(e);t.target==="openapi-3.0"?(i.ref=o.innerType,n.nullable=!0):n.anyOf=[s,{type:"null"}]},nonoptionalProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType},defaultProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType,n.default=JSON.parse(JSON.stringify(o.defaultValue))},prefaultProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType,t.io==="input"&&(n._prefault=JSON.parse(JSON.stringify(o.defaultValue)))},catchProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType;let i;try{i=o.catchValue(void 0)}catch{throw new Error("Dynamic catch values are not supported in JSON Schema")}n.default=i},pipeProcessor=(e,t,n,r)=>{const o=e._zod.def,s=o.in._zod.traits.has("$ZodTransform"),i=t.io==="input"?s?o.out:o.in:o.out;process(i,t,r);const a=t.seen.get(e);a.ref=i},readonlyProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType,n.readOnly=!0},promiseProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType},optionalProcessor=(e,t,n,r)=>{const o=e._zod.def;process(o.innerType,t,r);const s=t.seen.get(e);s.ref=o.innerType},lazyProcessor=(e,t,n,r)=>{const o=e._zod.innerType;process(o,t,r);const s=t.seen.get(e);s.ref=o},allProcessors={string:stringProcessor,number:numberProcessor,boolean:booleanProcessor,bigint:bigintProcessor,symbol:symbolProcessor,null:nullProcessor,undefined:undefinedProcessor,void:voidProcessor,never:neverProcessor,any:anyProcessor,unknown:unknownProcessor,date:dateProcessor,enum:enumProcessor,literal:literalProcessor,nan:nanProcessor,template_literal:templateLiteralProcessor,file:fileProcessor,success:successProcessor,custom:customProcessor,function:functionProcessor,transform:transformProcessor,map:mapProcessor,set:setProcessor,array:arrayProcessor,object:objectProcessor,union:unionProcessor,intersection:intersectionProcessor,tuple:tupleProcessor,record:recordProcessor,nullable:nullableProcessor,nonoptional:nonoptionalProcessor,default:defaultProcessor,prefault:prefaultProcessor,catch:catchProcessor,pipe:pipeProcessor,readonly:readonlyProcessor,promise:promiseProcessor,optional:optionalProcessor,lazy:lazyProcessor};function toJSONSchema(e,t){if("_idmap"in e){const r=e,o=initializeContext({...t,processors:allProcessors}),s={};for(const l of r._idmap.entries()){const[d,u]=l;process(u,o)}const i={},a={registry:r,uri:t==null?void 0:t.uri,defs:s};o.external=a;for(const l of r._idmap.entries()){const[d,u]=l;extractDefs(o,u),i[d]=finalize(o,u)}if(Object.keys(s).length>0){const l=o.target==="draft-2020-12"?"$defs":"definitions";i.__shared={[l]:s}}return{schemas:i}}const n=initializeContext({...t,processors:allProcessors});return process(e,n),extractDefs(n,e),finalize(n,e)}const ZodISODateTime=$constructor("ZodISODateTime",(e,t)=>{$ZodISODateTime.init(e,t),ZodStringFormat.init(e,t)});function datetime(e){return _isoDateTime(ZodISODateTime,e)}const ZodISODate=$constructor("ZodISODate",(e,t)=>{$ZodISODate.init(e,t),ZodStringFormat.init(e,t)});function date(e){return _isoDate(ZodISODate,e)}const ZodISOTime=$constructor("ZodISOTime",(e,t)=>{$ZodISOTime.init(e,t),ZodStringFormat.init(e,t)});function time(e){return _isoTime(ZodISOTime,e)}const ZodISODuration=$constructor("ZodISODuration",(e,t)=>{$ZodISODuration.init(e,t),ZodStringFormat.init(e,t)});function duration(e){return _isoDuration(ZodISODuration,e)}const initializer=(e,t)=>{$ZodError.init(e,t),e.name="ZodError",Object.defineProperties(e,{format:{value:n=>formatError(e,n)},flatten:{value:n=>flattenError(e,n)},addIssue:{value:n=>{e.issues.push(n),e.message=JSON.stringify(e.issues,jsonStringifyReplacer,2)}},addIssues:{value:n=>{e.issues.push(...n),e.message=JSON.stringify(e.issues,jsonStringifyReplacer,2)}},isEmpty:{get(){return e.issues.length===0}}})},ZodRealError=$constructor("ZodError",initializer,{Parent:Error}),parse=_parse(ZodRealError),parseAsync=_parseAsync(ZodRealError),safeParse=_safeParse(ZodRealError),safeParseAsync=_safeParseAsync(ZodRealError),encode=_encode(ZodRealError),decode=_decode(ZodRealError),encodeAsync=_encodeAsync(ZodRealError),decodeAsync=_decodeAsync(ZodRealError),safeEncode=_safeEncode(ZodRealError),safeDecode=_safeDecode(ZodRealError),safeEncodeAsync=_safeEncodeAsync(ZodRealError),safeDecodeAsync=_safeDecodeAsync(ZodRealError),_installedGroups=new WeakMap;function _installLazyMethods(e,t,n){const r=Object.getPrototypeOf(e);let o=_installedGroups.get(r);if(o||(o=new Set,_installedGroups.set(r,o)),!o.has(t)){o.add(t);for(const s in n){const i=n[s];Object.defineProperty(r,s,{configurable:!0,enumerable:!1,get(){const a=i.bind(this);return Object.defineProperty(this,s,{configurable:!0,writable:!0,enumerable:!0,value:a}),a},set(a){Object.defineProperty(this,s,{configurable:!0,writable:!0,enumerable:!0,value:a})}})}}}const ZodType=$constructor("ZodType",(e,t)=>($ZodType.init(e,t),Object.assign(e["~standard"],{jsonSchema:{input:createStandardJSONSchemaMethod(e,"input"),output:createStandardJSONSchemaMethod(e,"output")}}),e.toJSONSchema=createToJSONSchemaMethod(e,{}),e.def=t,e.type=t.type,Object.defineProperty(e,"_def",{value:t}),e.parse=(n,r)=>parse(e,n,r,{callee:e.parse}),e.safeParse=(n,r)=>safeParse(e,n,r),e.parseAsync=async(n,r)=>parseAsync(e,n,r,{callee:e.parseAsync}),e.safeParseAsync=async(n,r)=>safeParseAsync(e,n,r),e.spa=e.safeParseAsync,e.encode=(n,r)=>encode(e,n,r),e.decode=(n,r)=>decode(e,n,r),e.encodeAsync=async(n,r)=>encodeAsync(e,n,r),e.decodeAsync=async(n,r)=>decodeAsync(e,n,r),e.safeEncode=(n,r)=>safeEncode(e,n,r),e.safeDecode=(n,r)=>safeDecode(e,n,r),e.safeEncodeAsync=async(n,r)=>safeEncodeAsync(e,n,r),e.safeDecodeAsync=async(n,r)=>safeDecodeAsync(e,n,r),_installLazyMethods(e,"ZodType",{check(...n){const r=this.def;return this.clone(mergeDefs(r,{checks:[...r.checks??[],...n.map(o=>typeof o=="function"?{_zod:{check:o,def:{check:"custom"},onattach:[]}}:o)]}),{parent:!0})},with(...n){return this.check(...n)},clone(n,r){return clone(this,n,r)},brand(){return this},register(n,r){return n.add(this,r),this},refine(n,r){return this.check(refine(n,r))},superRefine(n,r){return this.check(superRefine(n,r))},overwrite(n){return this.check(_overwrite(n))},optional(){return optional(this)},exactOptional(){return exactOptional(this)},nullable(){return nullable(this)},nullish(){return optional(nullable(this))},nonoptional(n){return nonoptional(this,n)},array(){return array(this)},or(n){return union([this,n])},and(n){return intersection(this,n)},transform(n){return pipe(this,transform(n))},default(n){return _default(this,n)},prefault(n){return prefault(this,n)},catch(n){return _catch(this,n)},pipe(n){return pipe(this,n)},readonly(){return readonly(this)},describe(n){const r=this.clone();return globalRegistry.add(r,{description:n}),r},meta(...n){if(n.length===0)return globalRegistry.get(this);const r=this.clone();return globalRegistry.add(r,n[0]),r},isOptional(){return this.safeParse(void 0).success},isNullable(){return this.safeParse(null).success},apply(n){return n(this)}}),Object.defineProperty(e,"description",{get(){var n;return(n=globalRegistry.get(e))==null?void 0:n.description},configurable:!0}),e)),_ZodString=$constructor("_ZodString",(e,t)=>{$ZodString.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(r,o,s)=>stringProcessor(e,r,o);const n=e._zod.bag;e.format=n.format??null,e.minLength=n.minimum??null,e.maxLength=n.maximum??null,_installLazyMethods(e,"_ZodString",{regex(...r){return this.check(_regex(...r))},includes(...r){return this.check(_includes(...r))},startsWith(...r){return this.check(_startsWith(...r))},endsWith(...r){return this.check(_endsWith(...r))},min(...r){return this.check(_minLength(...r))},max(...r){return this.check(_maxLength(...r))},length(...r){return this.check(_length(...r))},nonempty(...r){return this.check(_minLength(1,...r))},lowercase(r){return this.check(_lowercase(r))},uppercase(r){return this.check(_uppercase(r))},trim(){return this.check(_trim())},normalize(...r){return this.check(_normalize(...r))},toLowerCase(){return this.check(_toLowerCase())},toUpperCase(){return this.check(_toUpperCase())},slugify(){return this.check(_slugify())}})}),ZodString=$constructor("ZodString",(e,t)=>{$ZodString.init(e,t),_ZodString.init(e,t),e.email=n=>e.check(_email(ZodEmail,n)),e.url=n=>e.check(_url(ZodURL,n)),e.jwt=n=>e.check(_jwt(ZodJWT,n)),e.emoji=n=>e.check(_emoji(ZodEmoji,n)),e.guid=n=>e.check(_guid(ZodGUID,n)),e.uuid=n=>e.check(_uuid(ZodUUID,n)),e.uuidv4=n=>e.check(_uuidv4(ZodUUID,n)),e.uuidv6=n=>e.check(_uuidv6(ZodUUID,n)),e.uuidv7=n=>e.check(_uuidv7(ZodUUID,n)),e.nanoid=n=>e.check(_nanoid(ZodNanoID,n)),e.guid=n=>e.check(_guid(ZodGUID,n)),e.cuid=n=>e.check(_cuid(ZodCUID,n)),e.cuid2=n=>e.check(_cuid2(ZodCUID2,n)),e.ulid=n=>e.check(_ulid(ZodULID,n)),e.base64=n=>e.check(_base64(ZodBase64,n)),e.base64url=n=>e.check(_base64url(ZodBase64URL,n)),e.xid=n=>e.check(_xid(ZodXID,n)),e.ksuid=n=>e.check(_ksuid(ZodKSUID,n)),e.ipv4=n=>e.check(_ipv4(ZodIPv4,n)),e.ipv6=n=>e.check(_ipv6(ZodIPv6,n)),e.cidrv4=n=>e.check(_cidrv4(ZodCIDRv4,n)),e.cidrv6=n=>e.check(_cidrv6(ZodCIDRv6,n)),e.e164=n=>e.check(_e164(ZodE164,n)),e.datetime=n=>e.check(datetime(n)),e.date=n=>e.check(date(n)),e.time=n=>e.check(time(n)),e.duration=n=>e.check(duration(n))});function string(e){return _string(ZodString,e)}const ZodStringFormat=$constructor("ZodStringFormat",(e,t)=>{$ZodStringFormat.init(e,t),_ZodString.init(e,t)}),ZodEmail=$constructor("ZodEmail",(e,t)=>{$ZodEmail.init(e,t),ZodStringFormat.init(e,t)}),ZodGUID=$constructor("ZodGUID",(e,t)=>{$ZodGUID.init(e,t),ZodStringFormat.init(e,t)}),ZodUUID=$constructor("ZodUUID",(e,t)=>{$ZodUUID.init(e,t),ZodStringFormat.init(e,t)}),ZodURL=$constructor("ZodURL",(e,t)=>{$ZodURL.init(e,t),ZodStringFormat.init(e,t)}),ZodEmoji=$constructor("ZodEmoji",(e,t)=>{$ZodEmoji.init(e,t),ZodStringFormat.init(e,t)}),ZodNanoID=$constructor("ZodNanoID",(e,t)=>{$ZodNanoID.init(e,t),ZodStringFormat.init(e,t)}),ZodCUID=$constructor("ZodCUID",(e,t)=>{$ZodCUID.init(e,t),ZodStringFormat.init(e,t)}),ZodCUID2=$constructor("ZodCUID2",(e,t)=>{$ZodCUID2.init(e,t),ZodStringFormat.init(e,t)}),ZodULID=$constructor("ZodULID",(e,t)=>{$ZodULID.init(e,t),ZodStringFormat.init(e,t)}),ZodXID=$constructor("ZodXID",(e,t)=>{$ZodXID.init(e,t),ZodStringFormat.init(e,t)}),ZodKSUID=$constructor("ZodKSUID",(e,t)=>{$ZodKSUID.init(e,t),ZodStringFormat.init(e,t)}),ZodIPv4=$constructor("ZodIPv4",(e,t)=>{$ZodIPv4.init(e,t),ZodStringFormat.init(e,t)}),ZodIPv6=$constructor("ZodIPv6",(e,t)=>{$ZodIPv6.init(e,t),ZodStringFormat.init(e,t)}),ZodCIDRv4=$constructor("ZodCIDRv4",(e,t)=>{$ZodCIDRv4.init(e,t),ZodStringFormat.init(e,t)}),ZodCIDRv6=$constructor("ZodCIDRv6",(e,t)=>{$ZodCIDRv6.init(e,t),ZodStringFormat.init(e,t)}),ZodBase64=$constructor("ZodBase64",(e,t)=>{$ZodBase64.init(e,t),ZodStringFormat.init(e,t)}),ZodBase64URL=$constructor("ZodBase64URL",(e,t)=>{$ZodBase64URL.init(e,t),ZodStringFormat.init(e,t)}),ZodE164=$constructor("ZodE164",(e,t)=>{$ZodE164.init(e,t),ZodStringFormat.init(e,t)}),ZodJWT=$constructor("ZodJWT",(e,t)=>{$ZodJWT.init(e,t),ZodStringFormat.init(e,t)}),ZodNumber=$constructor("ZodNumber",(e,t)=>{$ZodNumber.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(r,o,s)=>numberProcessor(e,r,o),_installLazyMethods(e,"ZodNumber",{gt(r,o){return this.check(_gt(r,o))},gte(r,o){return this.check(_gte(r,o))},min(r,o){return this.check(_gte(r,o))},lt(r,o){return this.check(_lt(r,o))},lte(r,o){return this.check(_lte(r,o))},max(r,o){return this.check(_lte(r,o))},int(r){return this.check(int(r))},safe(r){return this.check(int(r))},positive(r){return this.check(_gt(0,r))},nonnegative(r){return this.check(_gte(0,r))},negative(r){return this.check(_lt(0,r))},nonpositive(r){return this.check(_lte(0,r))},multipleOf(r,o){return this.check(_multipleOf(r,o))},step(r,o){return this.check(_multipleOf(r,o))},finite(){return this}});const n=e._zod.bag;e.minValue=Math.max(n.minimum??Number.NEGATIVE_INFINITY,n.exclusiveMinimum??Number.NEGATIVE_INFINITY)??null,e.maxValue=Math.min(n.maximum??Number.POSITIVE_INFINITY,n.exclusiveMaximum??Number.POSITIVE_INFINITY)??null,e.isInt=(n.format??"").includes("int")||Number.isSafeInteger(n.multipleOf??.5),e.isFinite=!0,e.format=n.format??null});function number(e){return _number(ZodNumber,e)}const ZodNumberFormat=$constructor("ZodNumberFormat",(e,t)=>{$ZodNumberFormat.init(e,t),ZodNumber.init(e,t)});function int(e){return _int(ZodNumberFormat,e)}const ZodBoolean=$constructor("ZodBoolean",(e,t)=>{$ZodBoolean.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>booleanProcessor(e,n,r)});function boolean(e){return _boolean(ZodBoolean,e)}const ZodUnknown=$constructor("ZodUnknown",(e,t)=>{$ZodUnknown.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>unknownProcessor()});function unknown(){return _unknown(ZodUnknown)}const ZodNever=$constructor("ZodNever",(e,t)=>{$ZodNever.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>neverProcessor(e,n,r)});function never(e){return _never(ZodNever,e)}const ZodArray=$constructor("ZodArray",(e,t)=>{$ZodArray.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>arrayProcessor(e,n,r,o),e.element=t.element,_installLazyMethods(e,"ZodArray",{min(n,r){return this.check(_minLength(n,r))},nonempty(n){return this.check(_minLength(1,n))},max(n,r){return this.check(_maxLength(n,r))},length(n,r){return this.check(_length(n,r))},unwrap(){return this.element}})});function array(e,t){return _array(ZodArray,e,t)}const ZodObject=$constructor("ZodObject",(e,t)=>{$ZodObjectJIT.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>objectProcessor(e,n,r,o),defineLazy(e,"shape",()=>t.shape),_installLazyMethods(e,"ZodObject",{keyof(){return _enum(Object.keys(this._zod.def.shape))},catchall(n){return this.clone({...this._zod.def,catchall:n})},passthrough(){return this.clone({...this._zod.def,catchall:unknown()})},loose(){return this.clone({...this._zod.def,catchall:unknown()})},strict(){return this.clone({...this._zod.def,catchall:never()})},strip(){return this.clone({...this._zod.def,catchall:void 0})},extend(n){return extend(this,n)},safeExtend(n){return safeExtend(this,n)},merge(n){return merge(this,n)},pick(n){return pick(this,n)},omit(n){return omit(this,n)},partial(...n){return partial(ZodOptional,this,n[0])},required(...n){return required(ZodNonOptional,this,n[0])}})});function object(e,t){const n={type:"object",shape:e??{},...normalizeParams(t)};return new ZodObject(n)}const ZodUnion=$constructor("ZodUnion",(e,t)=>{$ZodUnion.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>unionProcessor(e,n,r,o),e.options=t.options});function union(e,t){return new ZodUnion({type:"union",options:e,...normalizeParams(t)})}const ZodIntersection=$constructor("ZodIntersection",(e,t)=>{$ZodIntersection.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>intersectionProcessor(e,n,r,o)});function intersection(e,t){return new ZodIntersection({type:"intersection",left:e,right:t})}const ZodEnum=$constructor("ZodEnum",(e,t)=>{$ZodEnum.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(r,o,s)=>enumProcessor(e,r,o),e.enum=t.entries,e.options=Object.values(t.entries);const n=new Set(Object.keys(t.entries));e.extract=(r,o)=>{const s={};for(const i of r)if(n.has(i))s[i]=t.entries[i];else throw new Error(`Key ${i} not found in enum`);return new ZodEnum({...t,checks:[],...normalizeParams(o),entries:s})},e.exclude=(r,o)=>{const s={...t.entries};for(const i of r)if(n.has(i))delete s[i];else throw new Error(`Key ${i} not found in enum`);return new ZodEnum({...t,checks:[],...normalizeParams(o),entries:s})}});function _enum(e,t){const n=Array.isArray(e)?Object.fromEntries(e.map(r=>[r,r])):e;return new ZodEnum({type:"enum",entries:n,...normalizeParams(t)})}const ZodTransform=$constructor("ZodTransform",(e,t)=>{$ZodTransform.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>transformProcessor(e,n),e._zod.parse=(n,r)=>{if(r.direction==="backward")throw new $ZodEncodeError(e.constructor.name);n.addIssue=s=>{if(typeof s=="string")n.issues.push(issue(s,n.value,t));else{const i=s;i.fatal&&(i.continue=!1),i.code??(i.code="custom"),i.input??(i.input=n.value),i.inst??(i.inst=e),n.issues.push(issue(i))}};const o=t.transform(n.value,n);return o instanceof Promise?o.then(s=>(n.value=s,n.fallback=!0,n)):(n.value=o,n.fallback=!0,n)}});function transform(e){return new ZodTransform({type:"transform",transform:e})}const ZodOptional=$constructor("ZodOptional",(e,t)=>{$ZodOptional.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>optionalProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType});function optional(e){return new ZodOptional({type:"optional",innerType:e})}const ZodExactOptional=$constructor("ZodExactOptional",(e,t)=>{$ZodExactOptional.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>optionalProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType});function exactOptional(e){return new ZodExactOptional({type:"optional",innerType:e})}const ZodNullable=$constructor("ZodNullable",(e,t)=>{$ZodNullable.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>nullableProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType});function nullable(e){return new ZodNullable({type:"nullable",innerType:e})}const ZodDefault=$constructor("ZodDefault",(e,t)=>{$ZodDefault.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>defaultProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType,e.removeDefault=e.unwrap});function _default(e,t){return new ZodDefault({type:"default",innerType:e,get defaultValue(){return typeof t=="function"?t():shallowClone(t)}})}const ZodPrefault=$constructor("ZodPrefault",(e,t)=>{$ZodPrefault.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>prefaultProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType});function prefault(e,t){return new ZodPrefault({type:"prefault",innerType:e,get defaultValue(){return typeof t=="function"?t():shallowClone(t)}})}const ZodNonOptional=$constructor("ZodNonOptional",(e,t)=>{$ZodNonOptional.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>nonoptionalProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType});function nonoptional(e,t){return new ZodNonOptional({type:"nonoptional",innerType:e,...normalizeParams(t)})}const ZodCatch=$constructor("ZodCatch",(e,t)=>{$ZodCatch.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>catchProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType,e.removeCatch=e.unwrap});function _catch(e,t){return new ZodCatch({type:"catch",innerType:e,catchValue:typeof t=="function"?t:()=>t})}const ZodPipe=$constructor("ZodPipe",(e,t)=>{$ZodPipe.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>pipeProcessor(e,n,r,o),e.in=t.in,e.out=t.out});function pipe(e,t){return new ZodPipe({type:"pipe",in:e,out:t})}const ZodReadonly=$constructor("ZodReadonly",(e,t)=>{$ZodReadonly.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>readonlyProcessor(e,n,r,o),e.unwrap=()=>e._zod.def.innerType});function readonly(e){return new ZodReadonly({type:"readonly",innerType:e})}const ZodCustom=$constructor("ZodCustom",(e,t)=>{$ZodCustom.init(e,t),ZodType.init(e,t),e._zod.processJSONSchema=(n,r,o)=>customProcessor(e,n)});function refine(e,t={}){return _refine(ZodCustom,e,t)}function superRefine(e,t){return _superRefine(e,t)}const ANSI_BACKGROUND_OFFSET=10,wrapAnsi16=(e=0)=>t=>`\x1B[${t+e}m`,wrapAnsi256=(e=0)=>t=>`\x1B[${38+e};5;${t}m`,wrapAnsi16m=(e=0)=>(t,n,r)=>`\x1B[${38+e};2;${t};${n};${r}m`,styles$1={modifier:{reset:[0,0],bold:[1,22],dim:[2,22],italic:[3,23],underline:[4,24],overline:[53,55],inverse:[7,27],hidden:[8,28],strikethrough:[9,29]},color:{black:[30,39],red:[31,39],green:[32,39],yellow:[33,39],blue:[34,39],magenta:[35,39],cyan:[36,39],white:[37,39],blackBright:[90,39],gray:[90,39],grey:[90,39],redBright:[91,39],greenBright:[92,39],yellowBright:[93,39],blueBright:[94,39],magentaBright:[95,39],cyanBright:[96,39],whiteBright:[97,39]},bgColor:{bgBlack:[40,49],bgRed:[41,49],bgGreen:[42,49],bgYellow:[43,49],bgBlue:[44,49],bgMagenta:[45,49],bgCyan:[46,49],bgWhite:[47,49],bgBlackBright:[100,49],bgGray:[100,49],bgGrey:[100,49],bgRedBright:[101,49],bgGreenBright:[102,49],bgYellowBright:[103,49],bgBlueBright:[104,49],bgMagentaBright:[105,49],bgCyanBright:[106,49],bgWhiteBright:[107,49]}};Object.keys(styles$1.modifier);const foregroundColorNames=Object.keys(styles$1.color),backgroundColorNames=Object.keys(styles$1.bgColor);[...foregroundColorNames,...backgroundColorNames];function assembleStyles(){const e=new Map;for(const[t,n]of Object.entries(styles$1)){for(const[r,o]of Object.entries(n))styles$1[r]={open:`\x1B[${o[0]}m`,close:`\x1B[${o[1]}m`},n[r]=styles$1[r],e.set(o[0],o[1]);Object.defineProperty(styles$1,t,{value:n,enumerable:!1})}return Object.defineProperty(styles$1,"codes",{value:e,enumerable:!1}),styles$1.color.close="\x1B[39m",styles$1.bgColor.close="\x1B[49m",styles$1.color.ansi=wrapAnsi16(),styles$1.color.ansi256=wrapAnsi256(),styles$1.color.ansi16m=wrapAnsi16m(),styles$1.bgColor.ansi=wrapAnsi16(ANSI_BACKGROUND_OFFSET),styles$1.bgColor.ansi256=wrapAnsi256(ANSI_BACKGROUND_OFFSET),styles$1.bgColor.ansi16m=wrapAnsi16m(ANSI_BACKGROUND_OFFSET),Object.defineProperties(styles$1,{rgbToAnsi256:{value(t,n,r){return t===n&&n===r?t<8?16:t>248?231:Math.round((t-8)/247*24)+232:16+36*Math.round(t/255*5)+6*Math.round(n/255*5)+Math.round(r/255*5)},enumerable:!1},hexToRgb:{value(t){const n=/[a-f\d]{6}|[a-f\d]{3}/i.exec(t.toString(16));if(!n)return[0,0,0];let[r]=n;r.length===3&&(r=[...r].map(s=>s+s).join(""));const o=Number.parseInt(r,16);return[o>>16&255,o>>8&255,o&255]},enumerable:!1},hexToAnsi256:{value:t=>styles$1.rgbToAnsi256(...styles$1.hexToRgb(t)),enumerable:!1},ansi256ToAnsi:{value(t){if(t<8)return 30+t;if(t<16)return 90+(t-8);let n,r,o;if(t>=232)n=((t-232)*10+8)/255,r=n,o=n;else{t-=16;const a=t%36;n=Math.floor(t/36)/5,r=Math.floor(a/6)/5,o=a%6/5}const s=Math.max(n,r,o)*2;if(s===0)return 30;let i=30+(Math.round(o)<<2|Math.round(r)<<1|Math.round(n));return s===2&&(i+=60),i},enumerable:!1},rgbToAnsi:{value:(t,n,r)=>styles$1.ansi256ToAnsi(styles$1.rgbToAnsi256(t,n,r)),enumerable:!1},hexToAnsi:{value:t=>styles$1.ansi256ToAnsi(styles$1.hexToAnsi256(t)),enumerable:!1}}),styles$1}const ansiStyles=assembleStyles(),level=(()=>{if(!("navigator"in globalThis))return 0;if(globalThis.navigator.userAgentData){const e=navigator.userAgentData.brands.find(({brand:t})=>t==="Chromium");if(e&&e.version>93)return 3}return/\b(Chrome|Chromium)\//.test(globalThis.navigator.userAgent)?1:0})(),colorSupport=level!==0&&{level},supportsColor={stdout:colorSupport,stderr:colorSupport};function stringReplaceAll(e,t,n){let r=e.indexOf(t);if(r===-1)return e;const o=t.length;let s=0,i="";do i+=e.slice(s,r)+t+n,s=r+o,r=e.indexOf(t,s);while(r!==-1);return i+=e.slice(s),i}function stringEncaseCRLFWithFirstIndex(e,t,n,r){let o=0,s="";do{const i=e[r-1]==="\r";s+=e.slice(o,i?r-1:r)+t+(i?`\r
`:`
`)+n,o=r+1,r=e.indexOf(`
`,o)}while(r!==-1);return s+=e.slice(o),s}const{stdout:stdoutColor,stderr:stderrColor}=supportsColor,GENERATOR=Symbol("GENERATOR"),STYLER=Symbol("STYLER"),IS_EMPTY=Symbol("IS_EMPTY"),levelMapping=["ansi","ansi","ansi256","ansi16m"],styles=Object.create(null),applyOptions=(e,t={})=>{if(t.level&&!(Number.isInteger(t.level)&&t.level>=0&&t.level<=3))throw new Error("The `level` option should be an integer from 0 to 3");const n=stdoutColor?stdoutColor.level:0;e.level=t.level===void 0?n:t.level},chalkFactory=e=>{const t=(...n)=>n.join(" ");return applyOptions(t,e),Object.setPrototypeOf(t,createChalk.prototype),t};function createChalk(e){return chalkFactory(e)}Object.setPrototypeOf(createChalk.prototype,Function.prototype);for(const[e,t]of Object.entries(ansiStyles))styles[e]={get(){const n=createBuilder(this,createStyler(t.open,t.close,this[STYLER]),this[IS_EMPTY]);return Object.defineProperty(this,e,{value:n}),n}};styles.visible={get(){const e=createBuilder(this,this[STYLER],!0);return Object.defineProperty(this,"visible",{value:e}),e}};const getModelAnsi=(e,t,n,...r)=>e==="rgb"?t==="ansi16m"?ansiStyles[n].ansi16m(...r):t==="ansi256"?ansiStyles[n].ansi256(ansiStyles.rgbToAnsi256(...r)):ansiStyles[n].ansi(ansiStyles.rgbToAnsi(...r)):e==="hex"?getModelAnsi("rgb",t,n,...ansiStyles.hexToRgb(...r)):ansiStyles[n][e](...r),usedModels=["rgb","hex","ansi256"];for(const e of usedModels){styles[e]={get(){const{level:n}=this;return function(...r){const o=createStyler(getModelAnsi(e,levelMapping[n],"color",...r),ansiStyles.color.close,this[STYLER]);return createBuilder(this,o,this[IS_EMPTY])}}};const t="bg"+e[0].toUpperCase()+e.slice(1);styles[t]={get(){const{level:n}=this;return function(...r){const o=createStyler(getModelAnsi(e,levelMapping[n],"bgColor",...r),ansiStyles.bgColor.close,this[STYLER]);return createBuilder(this,o,this[IS_EMPTY])}}}}const proto=Object.defineProperties(()=>{},{...styles,level:{enumerable:!0,get(){return this[GENERATOR].level},set(e){this[GENERATOR].level=e}}}),createStyler=(e,t,n)=>{let r,o;return n===void 0?(r=e,o=t):(r=n.openAll+e,o=t+n.closeAll),{open:e,close:t,openAll:r,closeAll:o,parent:n}},createBuilder=(e,t,n)=>{const r=(...o)=>applyStyle(r,o.length===1?""+o[0]:o.join(" "));return Object.setPrototypeOf(r,proto),r[GENERATOR]=e,r[STYLER]=t,r[IS_EMPTY]=n,r},applyStyle=(e,t)=>{if(e.level<=0||!t)return e[IS_EMPTY]?"":t;let n=e[STYLER];if(n===void 0)return t;const{openAll:r,closeAll:o}=n;if(t.includes("\x1B"))for(;n!==void 0;)t=stringReplaceAll(t,n.close,n.open),n=n.parent;const s=t.indexOf(`
`);return s!==-1&&(t=stringEncaseCRLFWithFirstIndex(t,o,r,s)),r+t+o};Object.defineProperties(createChalk.prototype,styles);const chalk=createChalk();createChalk({level:stderrColor?stderrColor.level:0});var InvokeErrorTypes={NETWORK_ERROR:"network_error",RATE_LIMIT:"rate_limit",SERVER_ERROR:"server_error",NO_TOOL_CALL:"no_tool_call",INVALID_TOOL_ARGS:"invalid_tool_args",TOOL_EXECUTION_ERROR:"tool_execution_error",INVALID_RESPONSE:"invalid_response",INVALID_SCHEMA:"invalid_schema",UNKNOWN:"unknown",CONFIG_ERROR:"config_error",AUTH_ERROR:"auth_error",CONTEXT_LENGTH:"context_length",CONTENT_FILTER:"content_filter"},RETRYABLE_TYPES=[InvokeErrorTypes.NETWORK_ERROR,InvokeErrorTypes.RATE_LIMIT,InvokeErrorTypes.SERVER_ERROR,InvokeErrorTypes.NO_TOOL_CALL,InvokeErrorTypes.INVALID_TOOL_ARGS,InvokeErrorTypes.TOOL_EXECUTION_ERROR,InvokeErrorTypes.INVALID_RESPONSE,InvokeErrorTypes.INVALID_SCHEMA,InvokeErrorTypes.UNKNOWN],InvokeError=class extends Error{constructor(t,n,r,o){super(n);R(this,"type");R(this,"retryable");R(this,"statusCode");R(this,"rawError");R(this,"rawResponse");this.name="InvokeError",this.type=t,this.retryable=RETRYABLE_TYPES.includes(t),this.rawError=r,this.rawResponse=o}},debug=console.debug.bind(console,chalk.gray("[LLM]"));function zodToOpenAITool(e,t){return{type:"function",function:{name:e,description:t.description,parameters:toJSONSchema(t.inputSchema,{target:"openapi-3.0"})}}}function modelPatch(e){var r,o;const t=e.model||"";if(!t)return e;const n=normalizeModelName(t);return n.startsWith("qwen")&&(debug("Applying Qwen patch: use higher temperature for auto fixing"),e.temperature=Math.max(e.temperature||0,1),e.enable_thinking=!1),n.startsWith("claude")&&(debug("Applying Claude patch: disable thinking"),e.thinking={type:"disabled"},e.tool_choice==="required"?(debug('Applying Claude patch: convert tool_choice "required" to { type: "any" }'),e.tool_choice={type:"any"}):(o=(r=e.tool_choice)==null?void 0:r.function)!=null&&o.name&&(debug("Applying Claude patch: convert tool_choice format"),e.tool_choice={type:"tool",name:e.tool_choice.function.name}),(n.startsWith("claude-opus-4-7")||n.startsWith("claude-opus-47")||n.startsWith("claude-opus-4-8")||n.startsWith("claude-opus-48"))&&(debug("Applying Claude-4.7/4.8 patch: remove temperature"),delete e.temperature)),n.startsWith("grok")&&(debug("Applying Grok patch: removing tool_choice"),delete e.tool_choice,debug("Applying Grok patch: disable reasoning and thinking"),e.thinking={type:"disabled",effort:"minimal"},e.reasoning={enabled:!1,effort:"low"}),n.startsWith("gpt")&&(debug("Applying GPT patch: set verbosity to low"),e.verbosity="low",n.startsWith("gpt-52")?(debug("Applying GPT-52 patch: disable reasoning"),e.reasoning_effort="none"):n.startsWith("gpt-51")?(debug("Applying GPT-51 patch: disable reasoning"),e.reasoning_effort="none"):n.startsWith("gpt-54")?(debug("Applying GPT-5.4 patch: remove reasoning_effort"),delete e.reasoning_effort):n.startsWith("gpt-55")?(debug("Applying GPT-5.4 patch: remove reasoning_effort and temperature"),delete e.reasoning_effort,delete e.temperature):n.startsWith("gpt-5-mini")?(debug("Applying GPT-5-mini patch: set reasoning effort to low, temperature to 1"),e.reasoning_effort="low",e.temperature=1):n.startsWith("gpt-5")&&(debug("Applying GPT-5 patch: set reasoning effort to low"),e.reasoning_effort="low")),n.startsWith("gemini")&&(debug("Applying Gemini patch: set reasoning effort to minimal"),e.reasoning_effort="minimal"),n.startsWith("deepseek")&&(debug("Applying DeepSeek patch: remove tool_choice"),delete e.tool_choice),n.startsWith("minimax")&&(debug("Applying MiniMax patch: clamp temperature to (0, 1]"),e.temperature=Math.max(e.temperature||0,.01),e.temperature>1&&(e.temperature=1),delete e.parallel_tool_calls),e}function normalizeModelName(e){let t=e.toLowerCase();return t.includes("/")&&(t=t.split("/")[1]),t=t.replace(/_/g,""),t=t.replace(/\./g,""),t}var OpenAIClient=class{constructor(e){R(this,"config");R(this,"fetch");this.config=e,this.fetch=e.customFetch}async invoke(e,t,n,r){var U,Y,O,M,V,X,Q,W,ye,te,ue,ce,we,He,Je,de,qe,c;n==null||n.throwIfAborted();const o=Object.entries(t).map(([f,$])=>zodToOpenAITool(f,$));let s="required";r!=null&&r.toolChoiceName&&!this.config.disableNamedToolChoice&&(s={type:"function",function:{name:r.toolChoiceName}});const i={model:this.config.model,temperature:this.config.temperature,messages:e,tools:o,parallel_tool_calls:!1,tool_choice:s};modelPatch(i);let a;try{a=this.config.transformRequestBody(i)}catch(f){throw new InvokeError(InvokeErrorTypes.CONFIG_ERROR,`transformRequestBody failed: ${f.message}`,f)}const l=a??i;let d;try{d=await this.fetch(`${this.config.baseURL}/chat/completions`,{method:"POST",headers:{"Content-Type":"application/json",...this.config.apiKey&&{Authorization:`Bearer ${this.config.apiKey}`}},body:JSON.stringify(l),signal:n})}catch(f){throw(f==null?void 0:f.name)==="AbortError"?f:(console.error(f),new InvokeError(InvokeErrorTypes.NETWORK_ERROR,"Network request failed",f))}if(!d.ok){let f;try{f=await d.json()}catch(_){if((_==null?void 0:_.name)==="AbortError")throw _}const $=((U=f==null?void 0:f.error)==null?void 0:U.message)||d.statusText;throw d.status===401||d.status===403?new InvokeError(InvokeErrorTypes.AUTH_ERROR,`Authentication failed: ${$}`,f):d.status===429?new InvokeError(InvokeErrorTypes.RATE_LIMIT,`Rate limit exceeded: ${$}`,f):d.status>=500?new InvokeError(InvokeErrorTypes.SERVER_ERROR,`Server error: ${$}`,f):new InvokeError(InvokeErrorTypes.UNKNOWN,`HTTP ${d.status}: ${$}`,f)}let u;try{u=await d.json()}catch(f){throw(f==null?void 0:f.name)==="AbortError"?f:new InvokeError(InvokeErrorTypes.INVALID_RESPONSE,"Response body is not valid JSON",f)}const g=(Y=u.choices)==null?void 0:Y[0];if(!g)throw new InvokeError(InvokeErrorTypes.INVALID_SCHEMA,"No choices in response",u);switch(g.finish_reason){case"tool_calls":case"function_call":case"stop":break;case"length":throw new InvokeError(InvokeErrorTypes.CONTEXT_LENGTH,"Response truncated: max tokens reached",void 0,u);case"content_filter":throw new InvokeError(InvokeErrorTypes.CONTENT_FILTER,"Content filtered by safety system",void 0,u);default:throw new InvokeError(InvokeErrorTypes.INVALID_SCHEMA,`Unexpected finish_reason: ${g.finish_reason}`,void 0,u)}const h=(O=(r!=null&&r.normalizeResponse?r.normalizeResponse(u):u).choices)==null?void 0:O[0],m=(Q=(X=(V=(M=h==null?void 0:h.message)==null?void 0:M.tool_calls)==null?void 0:V[0])==null?void 0:X.function)==null?void 0:Q.name;if(!m)throw new InvokeError(InvokeErrorTypes.NO_TOOL_CALL,"No tool call found in response",void 0,u);const y=t[m];if(!y)throw new InvokeError(InvokeErrorTypes.UNKNOWN,`Tool "${m}" not found in tools`,void 0,u);const v=(ue=(te=(ye=(W=h.message)==null?void 0:W.tool_calls)==null?void 0:ye[0])==null?void 0:te.function)==null?void 0:ue.arguments;if(!v)throw new InvokeError(InvokeErrorTypes.INVALID_TOOL_ARGS,"No tool call arguments found",void 0,u);let T;try{T=JSON.parse(v)}catch(f){throw new InvokeError(InvokeErrorTypes.INVALID_TOOL_ARGS,"Failed to parse tool arguments as JSON",f,u)}const k=y.inputSchema.safeParse(T);if(!k.success)throw console.error(prettifyError(k.error)),new InvokeError(InvokeErrorTypes.INVALID_TOOL_ARGS,"Tool arguments validation failed",k.error,u);const S=k.data;let D;try{D=await y.execute(S)}catch(f){throw(f==null?void 0:f.name)==="AbortError"?f:new InvokeError(InvokeErrorTypes.TOOL_EXECUTION_ERROR,`Tool execution failed: ${f==null?void 0:f.message}`,f,u)}return{toolCall:{name:m,args:S},toolResult:D,usage:{promptTokens:((ce=u.usage)==null?void 0:ce.prompt_tokens)??0,completionTokens:((we=u.usage)==null?void 0:we.completion_tokens)??0,totalTokens:((He=u.usage)==null?void 0:He.total_tokens)??0,cachedTokens:(de=(Je=u.usage)==null?void 0:Je.prompt_tokens_details)==null?void 0:de.cached_tokens,reasoningTokens:(c=(qe=u.usage)==null?void 0:qe.completion_tokens_details)==null?void 0:c.reasoning_tokens},rawResponse:u,rawRequest:l}}};function parseLLMConfig(e){if(!e.baseURL||!e.model)throw new Error("[PageAgent] LLM configuration required. Please provide: baseURL, model. See: https://alibaba.github.io/page-agent/docs/features/models");return{baseURL:e.baseURL,model:e.model,apiKey:e.apiKey||"",temperature:e.temperature??.7,maxRetries:e.maxRetries??2,transformRequestBody:e.transformRequestBody??(t=>t),disableNamedToolChoice:e.disableNamedToolChoice??!1,customFetch:(e.customFetch??fetch).bind(globalThis)}}var LLM=class extends EventTarget{constructor(t){super();R(this,"config");R(this,"client");this.config=parseLLMConfig(t),this.client=new OpenAIClient(this.config)}async invoke(t,n,r,o){return await withRetry(async()=>this.client.invoke(t,n,r,o),{maxRetries:this.config.maxRetries,onRetry:(s,i)=>{this.dispatchEvent(new CustomEvent("retry",{detail:{attempt:s,maxAttempts:this.config.maxRetries,lastError:i}}))}})}};async function withRetry(e,t){let n=0;for(;;)try{return await e()}catch(r){if((r==null?void 0:r.name)==="AbortError"||r instanceof InvokeError&&!r.retryable||(n++,n>t.maxRetries))throw r;console.debug("[LLM] retryable failure, will retry:",r),t.onRetry(n,r),await new Promise(o=>setTimeout(o,100))}}var system_prompt_default=`You are an AI agent designed to operate in an iterative loop to automate browser tasks. Your ultimate goal is accomplishing the task provided in <user_request>.

<intro>
You excel at following tasks:
1. Navigating complex websites and extracting precise information
2. Automating form submissions and interactive web actions
3. Gathering and saving information 
4. Operate effectively in an agent loop
5. Efficiently performing diverse web tasks
</intro>

<language_settings>
- Default working language: **English**
- Use the language that user is using. Return in user's language.
</language_settings>

<input>
At every step, your input will consist of: 
1. <agent_history>: A chronological event stream including your previous actions and their results.
2. <agent_state>: Current <user_request> and <step_info>.
3. <browser_state>: Current URL, interactive elements indexed for actions, and visible page content.
</input>

<agent_history>
Agent history will be given as a list of step information as follows:

<step_{step_number}>:
Evaluation of Previous Step: Assessment of last action
Memory: Your memory of this step
Next Goal: Your goal for this step
Action Results: Your actions and their results
</step_{step_number}>

and system messages wrapped in <sys> tag.
</agent_history>

<user_request>
USER REQUEST: This is your ultimate objective and always remains visible.
- This has the highest priority. Make the user happy.
- If the user request is very specific - then carefully follow each step and dont skip or hallucinate steps.
- If the task is open ended you can plan yourself how to get it done.
</user_request>

<browser_state>
1. Browser State will be given as:

Current URL: URL of the page you are currently viewing.
Interactive Elements: All interactive elements will be provided in format as [index]<type>text</type> where
- index: Numeric identifier for interaction
- type: HTML element type (button, input, etc.)
- text: Element description

Examples:
[33]<div>User form</div>
\\t*[35]<button aria-label='Submit form'>Submit</button>

Note that:
- Only elements with numeric indexes in [] are interactive
- (stacked) indentation (with \\t) is important and means that the element is a (html) child of the element above (with a lower index)
- Elements tagged with \`*[\` are the new clickable elements that appeared on the website since the last step - if url has not changed.
- Pure text elements without [] are not interactive.
</browser_state>

<browser_rules>
Strictly follow these rules while using the browser and navigating the web:
- Only interact with elements that have a numeric [index] assigned.
- Only use indexes that are explicitly provided.
- If the page changes after, for example, an input text action, analyze if you need to interact with new elements, e.g. selecting the right option from the list.
- By default, only elements in the visible viewport are listed. Use scrolling actions if you suspect relevant content is offscreen which you need to interact with. Scroll ONLY if there are more pixels below or above the page.
- You can scroll by a specific number of pages using the num_pages parameter (e.g., 0.5 for half page, 2.0 for two pages).
- All the elements that are scrollable are marked with \`data-scrollable\` attribute. Including the scrollable distance in every directions. You can scroll *the element* in case some area are overflowed.
- If a captcha appears, tell user you can not solve captcha. Finish the task and ask user to solve it.
- If the page is not fully loaded, use the \`wait\` action.
- Do not repeat one action for more than 3 times unless some conditions changed.
- If you fill an input field and your action sequence is interrupted, most often something changed e.g. suggestions popped up under the field.
- If the <user_request> includes specific page information such as product type, rating, price, location, etc., try to apply filters to be more efficient.
- The <user_request> is the ultimate goal. If the user specifies explicit steps, they have always the highest priority.
- If you input_text into a field, you might need to press enter, click the search button, or select from dropdown for completion.
- Don't login into a page if you don't have to. Don't login if you don't have the credentials. 
- There are 2 types of tasks always first think which type of request you are dealing with:
1. Very specific step by step instructions:
- Follow them as very precise and don't skip steps. Try to complete everything as requested.
2. Open ended tasks. Plan yourself, be creative in achieving them.
- If you get stuck e.g. with logins or captcha in open-ended tasks you can re-evaluate the task and try alternative ways, e.g. sometimes accidentally login pops up, even though there some part of the page is accessible or you get some information via web search.
</browser_rules>

<capability>
- You can only handle single page app. Do not jump out of current page.
- Do not click on link if it will open in a new page (e.g., <a target="_blank">)
- It is ok to fail the task.
	- User can be wrong. If the request of user is not achievable, inappropriate or you do not have enough information or tools to achieve it. Tell user to make a better request.
	- Webpage can be broken. All webpages or apps have bugs. Some bug will make it hard for your job. It's encouraged to tell user the problem of current page. Your feedbacks (including failing) are valuable for user.
	- Trying too hard can be harmful. Repeating some action back and forth or pushing for a complex procedure with little knowledge can cause unwanted results and harmful side-effects. User would rather you complete the task with a fail.
- If you do not have knowledge for the current webpage or task. You must require user to give specific instructions and detailed steps.
</capability>

<task_completion_rules>
You must call the \`done\` action in one of three cases:
- When you have fully completed the USER REQUEST.
- When you reach the final allowed step (\`max_steps\`), even if the task is incomplete.
- When you feel stuck or unable to solve user request. Or user request is not clear or contains inappropriate content.
- If it is ABSOLUTELY IMPOSSIBLE to continue.

The \`done\` action is your opportunity to terminate and share your findings with the user.
- Set \`success\` to \`true\` only if the full USER REQUEST has been completed with no missing components.
- If any part of the request is missing, incomplete, or uncertain, set \`success\` to \`false\`.
- You can use the \`text\` field of the \`done\` action to communicate your findings and to provide a coherent reply to the user and fulfill the USER REQUEST.
- You are ONLY ALLOWED to call \`done\` as a single action. Don't call it together with other actions.
- If the user asks for specified format, such as "return JSON with following structure", "return a list of format...", MAKE sure to use the right format in your answer.
- If the user asks for a structured output, your \`done\` action's schema may be modified. Take this schema into account when solving the task!
</task_completion_rules>

<reasoning_rules>
Exhibit the following reasoning patterns to successfully achieve the <user_request>:

- Reason about <agent_history> to track progress and context toward <user_request>.
- Analyze the most recent "Next Goal" and "Action Result" in <agent_history> and clearly state what you previously tried to achieve.
- Analyze all relevant items in <agent_history> and <browser_state> to understand your state.
- Explicitly judge success/failure/uncertainty of the last action. Never assume an action succeeded just because it appears to be executed in your last step in <agent_history>. If the expected change is missing, mark the last action as failed (or uncertain) and plan a recovery.
- Analyze whether you are stuck, e.g. when you repeat the same actions multiple times without any progress. Then consider alternative approaches e.g. scrolling for more context or ask user for help.
- Ask user for help if you have any difficulty. Keep user in the loop.
- If you see information relevant to <user_request>, plan saving the information to memory.
- Always reason about the <user_request>. Make sure to carefully analyze the specific steps and information required. E.g. specific filters, specific form fields, specific information to search. Make sure to always compare the current trajectory with the user request and think carefully if thats how the user requested it.
</reasoning_rules>

<examples>
Here are examples of good output patterns. Use them as reference but never copy them directly.

<evaluation_examples>
"evaluation_previous_goal": "Successfully navigated to the product page and found the target information. Verdict: Success"
"evaluation_previous_goal": "Clicked the login button and user authentication form appeared. Verdict: Success"
</evaluation_examples>

<memory_examples>
"memory": "Found many pending reports that need to be analyzed in the main page. Successfully processed the first 2 reports on quarterly sales data and moving on to inventory analysis and customer feedback reports."
</memory_examples>

<next_goal_examples>
"next_goal": "Click on the 'Add to Cart' button to proceed with the purchase flow."
</next_goal_examples>
</examples>

<output>
{
  "evaluation_previous_goal": "Concise one-sentence analysis of your last action. Clearly state success, failure, or uncertain.",
  "memory": "1-3 concise sentences of specific memory of this step and overall progress. You should put here everything that will help you track progress in future steps. Like counting pages visited, items found, etc.",
  "next_goal": "State the next immediate goal and action to achieve it, in one clear sentence.",
  "action":{
    "Action name": {// Action parameters}
  }
}
</output>
`,log=console.log.bind(console,chalk.yellow("[autoFixer]"));function normalizeResponse(e,t){var i,a,l;let n;const r=(i=e.choices)==null?void 0:i[0];if(!r)throw new Error("No choices in response");const o=r.message;if(!o)throw new Error("No message in choice");const s=(a=o.tool_calls)==null?void 0:a[0];if((l=s==null?void 0:s.function)!=null&&l.arguments)n=safeJsonParse(s.function.arguments),s.function.name&&s.function.name!=="AgentOutput"&&(log("#1: fixing tool_call"),n={action:safeJsonParse(n)});else if(o.content){const d=retrieveJsonFromString(o.content.trim());if(d)n=safeJsonParse(d),(n==null?void 0:n.name)==="AgentOutput"&&(log("#2: fixing tool_call"),n=safeJsonParse(n.arguments)),(n==null?void 0:n.type)==="function"&&(log("#3: fixing tool_call"),n=safeJsonParse(n.function.arguments)),!(n!=null&&n.action)&&!(n!=null&&n.evaluation_previous_goal)&&!(n!=null&&n.memory)&&!(n!=null&&n.next_goal)&&!(n!=null&&n.thinking)&&(log("#4: fixing tool_call"),n={action:safeJsonParse(n)});else throw new Error("No tool_call and the message content does not contain valid JSON")}else throw new Error("No tool_call nor message content is present");return n=safeJsonParse(n),n.action&&(n.action=safeJsonParse(n.action)),n.action&&t&&(n.action=validateAction(n.action,t)),n.action||(log("#5: fixing tool_call"),n.action={wait:{seconds:1}}),{...e,choices:[{...r,message:{...o,tool_calls:[{...s||{},function:{...(s==null?void 0:s.function)||{},name:"AgentOutput",arguments:JSON.stringify(n)}}]}}]}}function validateAction(e,t){if(typeof e!="object"||e===null)return e;const n=Object.keys(e)[0];if(!n)return e;const r=t.get(n);if(!r){const a=Array.from(t.keys()).join(", ");throw new InvokeError(InvokeErrorTypes.INVALID_TOOL_ARGS,`Unknown action "${n}". Available: ${a}`)}let o=e[n];const s=r.inputSchema;if(s instanceof ZodObject&&o!==null&&typeof o!="object"){const a=Object.keys(s.shape).find(l=>!s.shape[l].safeParse(void 0).success);a&&(log(`coercing primitive action input for "${n}"`),o={[a]:o})}const i=s.safeParse(o);if(!i.success)throw new InvokeError(InvokeErrorTypes.INVALID_TOOL_ARGS,`Invalid input for action "${n}": ${prettifyError(i.error)}`);return{[n]:i.data}}function safeJsonParse(e){if(typeof e=="string")try{return JSON.parse(e.trim())}catch{return e}return e}function retrieveJsonFromString(e){try{const t=/({[\s\S]*})/.exec(e)??[];return t.length===0?null:JSON.parse(t[0])}catch{return null}}async function waitFor$1(e,t){if(!t){await new Promise(n=>setTimeout(n,e*1e3));return}t.throwIfAborted(),await new Promise((n,r)=>{const o=setTimeout(()=>{t.removeEventListener("abort",s),n()},e*1e3),s=()=>{clearTimeout(o),r(t.reason)};t.addEventListener("abort",s,{once:!0})})}function truncate$1(e,t){return e.length>t?e.substring(0,t)+"...":e}function randomID(e){let t=Math.random().toString(36).substring(2,11);if(!e)return t;const n=1e3;let r=0;for(;e.includes(t);)if(t=Math.random().toString(36).substring(2,11),r++,r>n)throw new Error("randomID: too many tries");return t}var _global=globalThis;_global.__PAGE_AGENT_IDS__||(_global.__PAGE_AGENT_IDS__=[]);var ids=_global.__PAGE_AGENT_IDS__;function uid(){const e=randomID(ids);return ids.push(e),e}var llmsTxtCache=new Map;async function fetchLlmsTxt(e){let t;try{t=new URL(e).origin}catch{return null}if(t==="null")return null;if(llmsTxtCache.has(t))return llmsTxtCache.get(t);const n=`${t}/llms.txt`;let r=null;try{console.log(chalk.gray(`[llms.txt] Fetching ${n}`));const o=await fetch(n,{signal:AbortSignal.timeout(3e3)});o.ok?(r=await o.text(),console.log(chalk.green(`[llms.txt] Found (${r.length} chars)`)),r.length>1e3&&(console.log(chalk.yellow("[llms.txt] Truncating to 1000 chars")),r=truncate$1(r,1e3))):console.debug(chalk.gray(`[llms.txt] ${o.status} for ${n}`))}catch(o){console.debug(chalk.gray(`[llms.txt] not found for ${n}`),o)}return llmsTxtCache.set(t,r),r}function assert(e,t,n){if(!e){const r=t??"Assertion failed";throw console.error(chalk.red(`❌ assert: ${r}`)),new Error(r)}}async function suppress(e){try{return await e()}catch(t){console.error(t);return}}function tool(e){return e}var tools=new Map;tools.set("done",{description:"Complete task. Text is your final response to the user — keep it concise unless the user explicitly asks for detail.",inputSchema:object({text:string(),success:boolean().default(!0)}),execute:async function(e){return Promise.resolve("Task completed")}});tools.set("wait",{description:"Wait for x seconds. Can be used to wait until the page or data is fully loaded.",inputSchema:object({seconds:number().min(1).max(10).default(1)}),execute:async function(e,{signal:t}){const n=await this.pageController.getLastUpdateTime(),r=(Date.now()-n)/1e3,o=Math.max(0,e.seconds-r);return console.log(`actualWaitTime: ${o} seconds`),await waitFor$1(o,t),`✅ Waited for ${(r+o).toFixed(2)} seconds.`}});tools.set("ask_user",{description:"Ask the user a question and wait for their answer. Use this if you need more information or clarification.",inputSchema:object({question:string()}),execute:async function(e,{signal:t}){if(!this.onAskUser)throw new Error("ask_user tool requires onAskUser callback to be set");return`User answered: ${await this.onAskUser(e.question,{signal:t})}`}});tools.set("click_element_by_index",{description:"Click element by index",inputSchema:object({index:int().min(0)}),execute:async function(e){return(await this.pageController.clickElement(e.index)).message}});tools.set("input_text",{description:"Click and type text into an interactive input element",inputSchema:object({index:int().min(0),text:string()}),execute:async function(e){return(await this.pageController.inputText(e.index,e.text)).message}});tools.set("select_dropdown_option",{description:"Select dropdown option for interactive element index by the text of the option you want to select",inputSchema:object({index:int().min(0),text:string()}),execute:async function(e){return(await this.pageController.selectOption(e.index,e.text)).message}});tools.set("scroll",{description:"Scroll vertically. Without index: scrolls the document. With index: scrolls the container at that index (or its nearest scrollable ancestor). Use index of a data-scrollable element to scroll a specific area.",inputSchema:object({down:boolean().default(!0),num_pages:number().min(0).max(10).optional().default(.1),pixels:number().int().min(0).optional(),index:number().int().min(0).optional()}),execute:async function(e){return(await this.pageController.scroll({...e,numPages:e.num_pages})).message}});tools.set("scroll_horizontally",{description:"Scroll horizontally. Without index: scrolls the document. With index: scrolls the container at that index (or its nearest scrollable ancestor). Use index of a data-scrollable element to scroll a specific area.",inputSchema:object({right:boolean().default(!0),pixels:number().int().min(0),index:number().int().min(0).optional()}),execute:async function(e){return(await this.pageController.scrollHorizontally(e)).message}});tools.set("execute_javascript",{description:"Execute JavaScript code on the current page. Supports async/await syntax. Use with caution! An `AbortSignal` named `signal` is available in scope: long-running async code MUST honor it (e.g. `await fetch(url, { signal })`, or `signal.throwIfAborted()` in loops)",inputSchema:object({script:string()}),execute:async function(e,{signal:t}){const n=await this.pageController.executeJavascript(e.script,t);return t.throwIfAborted(),n.message}});var ke,Ne,fe,$e,De,ze,ee,C,lt,Te,xe,Qe,ut,dt,ht,pt,ft,at,PageAgentCore=(at=class extends EventTarget{constructor(t){super();L(this,C);R(this,"id",uid());R(this,"config");R(this,"tools");R(this,"pageController");R(this,"task","");R(this,"taskId","");R(this,"history",[]);R(this,"disposed",!1);R(this,"onAskUser");L(this,ke,"idle");L(this,Ne);L(this,fe,new AbortController);L(this,$e,[]);L(this,De,Promise.resolve());L(this,ze,null);L(this,ee,{totalWaitTime:0,lastURL:"",browserState:null});if(this.config={...t,maxSteps:t.maxSteps??40},I(this,Ne,new LLM(this.config)),this.tools=new Map(tools),this.pageController=t.pageController,p(this,Ne).addEventListener("retry",n=>{const{attempt:r,maxAttempts:o,lastError:s}=n.detail;w(this,C,xe).call(this,{type:"retrying",attempt:r,maxAttempts:o}),this.history.push({type:"error",message:String(s),rawResponse:s.rawResponse}),this.history.push({type:"retry",message:`LLM retry attempt ${r} of ${o}`,attempt:r,maxAttempts:o}),w(this,C,Te).call(this)}),this.config.customTools)for(const[n,r]of Object.entries(this.config.customTools)){if(r===null){this.tools.delete(n);continue}this.tools.set(n,r)}this.config.experimentalScriptExecutionTool||this.tools.delete("execute_javascript")}get status(){return p(this,ke)}get lastResult(){return p(this,ze)}pushObservation(t){p(this,$e).push(t)}async stop(){p(this,ke)==="running"&&(p(this,fe).abort(),await p(this,De))}async execute(t){var m,y;if(this.disposed)throw new Error("PageAgent has been disposed. Create a new instance.");if(p(this,ke)==="running")throw new Error("A task is already running.");if(!t)throw new Error("Task is required");this.task=t,this.taskId=uid(),this.history=[],I(this,$e,[]),I(this,ee,{totalWaitTime:0,lastURL:"",browserState:null}),I(this,fe,new AbortController);const n=p(this,fe).signal;let r;I(this,De,new Promise(v=>r=v)),w(this,C,Qe).call(this,"running"),w(this,C,Te).call(this),this.onAskUser||this.tools.delete("ask_user");const o=this.config.onBeforeStep,s=this.config.onAfterStep,i=this.config.onBeforeTask,a=this.config.onAfterTask,l=this.config.stepDelay??.4,d=this.config.maxSteps;let u=0,g,h="error";await suppress(()=>this.pageController.showMask());try{for(await(i==null?void 0:i(this));;){await(o==null?void 0:o(this,u));try{console.group(`step: ${u}`),u>0&&await waitFor$1(l,n),n.throwIfAborted(),console.log(chalk.blue.bold("👀 Observing...")),p(this,ee).browserState=await this.pageController.getBrowserState(),await w(this,C,pt).call(this,u);const v=[{role:"system",content:w(this,C,dt).call(this)},{role:"user",content:await w(this,C,ft).call(this)}],T={AgentOutput:w(this,C,ut).call(this)};console.log(chalk.blue.bold("🧠 Thinking...")),w(this,C,xe).call(this,{type:"thinking"});const k=await p(this,Ne).invoke(v,T,n,{toolChoiceName:"AgentOutput",normalizeResponse:V=>normalizeResponse(V,this.tools)}),S=k.toolResult,D=S.input,U=S.output,Y={evaluation_previous_goal:D.evaluation_previous_goal,memory:D.memory,next_goal:D.next_goal},O=Object.keys(D.action)[0],M={name:O,input:D.action[O],output:U};if(w(this,C,Te).call(this,{type:"step",stepIndex:u,reflection:Y,action:M,usage:k.usage,rawResponse:k.rawResponse,rawRequest:k.rawRequest}),O==="done"){const V=((m=M.input)==null?void 0:m.success)??!1,X=((y=M.input)==null?void 0:y.text)||"no text provided";console.log(chalk.green.bold("Task completed"),V,X),g={success:V,data:X,history:this.history},I(this,ze,g),h="completed";break}}catch(v){const T=(v==null?void 0:v.name)==="AbortError";T||console.error("Task failed",v);const k=T?"Task aborted":String(v);w(this,C,xe).call(this,{type:"error",message:k}),w(this,C,Te).call(this,{type:"error",message:k,rawResponse:v}),g={success:!1,data:k,history:this.history},I(this,ze,g),h=T?"stopped":"error";break}finally{console.groupEnd(),await(s==null?void 0:s(this,this.history))}if(u++,u>d){const v="Step count exceeded maximum limit";console.error(v),w(this,C,xe).call(this,{type:"error",message:v}),w(this,C,Te).call(this,{type:"error",message:v}),g={success:!1,data:v,history:this.history},I(this,ze,g),h="error";break}}return await(a==null?void 0:a(this,g)),g}catch(v){throw w(this,C,xe).call(this,{type:"error",message:String(v)}),h="error",v}finally{await suppress(()=>this.pageController.cleanUpHighlights()),await suppress(()=>this.pageController.hideMask()),p(this,fe).abort(),r(),w(this,C,Qe).call(this,h)}}dispose(){var t,n;console.log("Disposing PageAgent..."),this.disposed=!0,this.pageController.dispose(),p(this,fe).abort(),this.dispatchEvent(new Event("dispose")),(n=(t=this.config).onDispose)==null||n.call(t,this)}},ke=new WeakMap,Ne=new WeakMap,fe=new WeakMap,$e=new WeakMap,De=new WeakMap,ze=new WeakMap,ee=new WeakMap,C=new WeakSet,lt=function(){this.dispatchEvent(new Event("statuschange"))},Te=function(t){t&&this.history.push(t),this.dispatchEvent(new Event("historychange"))},xe=function(t){this.dispatchEvent(new CustomEvent("activity",{detail:t}))},Qe=function(t){p(this,ke)!==t&&(I(this,ke,t),w(this,C,lt).call(this))},ut=function(){const t=this.tools,n=Array.from(t.entries()).map(([o,s])=>object({[o]:s.inputSchema}).describe(s.description)),r=union(n);return{description:"You MUST call this tool every step!",inputSchema:object({evaluation_previous_goal:string().optional(),memory:string().optional(),next_goal:string().optional(),action:r}),execute:async o=>{const s=p(this,fe).signal;s.throwIfAborted(),console.log(chalk.blue.bold("MacroTool input"),o);const i=o.action,a=Object.keys(i)[0],l=i[a],d=[];o.evaluation_previous_goal&&d.push(`✅: ${o.evaluation_previous_goal}`),o.memory&&d.push(`💾: ${o.memory}`),o.next_goal&&d.push(`🎯: ${o.next_goal}`);const u=d.length>0?d.join(`
`):"";u&&console.log(u);const g=t.get(a);assert(g,`Tool ${a} not found`),console.log(chalk.blue.bold(`Executing tool: ${a}`),l),w(this,C,xe).call(this,{type:"executing",tool:a,input:l});const h=Date.now(),m=await g.execute.bind(this)(l,{signal:s});s.throwIfAborted();const y=Date.now()-h;return console.log(chalk.green.bold(`Tool (${a}) executed for ${y}ms`),m),w(this,C,xe).call(this,{type:"executed",tool:a,input:l,output:m,duration:y}),a==="wait"?p(this,ee).totalWaitTime+=(l==null?void 0:l.seconds)||0:p(this,ee).totalWaitTime=0,{input:o,output:m}}}},dt=function(){if(this.config.customSystemPrompt)return this.config.customSystemPrompt;const t=this.config.language==="zh-CN"?"中文":"English";return system_prompt_default.replace(/Default working language: \*\*.*?\*\*/,`Default working language: **${t}**`)},ht=async function(){var l,d,u;const{instructions:t,experimentalLlmsTxt:n}=this.config,r=(l=t==null?void 0:t.system)==null?void 0:l.trim();let o;const s=((d=p(this,ee).browserState)==null?void 0:d.url)||"";if(t!=null&&t.getPageInstructions&&s)try{o=(u=t.getPageInstructions(s))==null?void 0:u.trim()}catch(g){console.error(chalk.red("[PageAgent] Failed to execute getPageInstructions callback:"),g)}const i=n&&s?await fetchLlmsTxt(s):void 0;if(!r&&!o&&!i)return"";let a=`<instructions>
`;return r&&(a+=`<system_instructions>
${r}
</system_instructions>
`),o&&(a+=`<page_instructions>
${o}
</page_instructions>
`),i&&(a+=`<llms_txt>
${i}
</llms_txt>
`),a+=`</instructions>

`,a},pt=async function(t){var o;p(this,ee).totalWaitTime>=3&&this.pushObservation(`You have waited ${p(this,ee).totalWaitTime} seconds accumulatively. DO NOT wait any longer unless you have a good reason.`);const n=((o=p(this,ee).browserState)==null?void 0:o.url)||"";n!==p(this,ee).lastURL&&(this.pushObservation(`Page navigated to → ${n}`),p(this,ee).lastURL=n,await waitFor$1(.5));const r=this.config.maxSteps-t;if(r===5?this.pushObservation(`⚠️ Only ${r} steps remaining. Consider wrapping up or calling done with partial results.`):r===2&&this.pushObservation(`⚠️ Critical: Only ${r} steps left! You must finish the task or call done immediately.`),p(this,$e).length>0){for(const s of p(this,$e))this.history.push({type:"observation",content:s}),console.log(chalk.cyan("Observation:"),s);I(this,$e,[]),w(this,C,Te).call(this)}},ft=async function(){const t=p(this,ee).browserState;let n="";n+=await w(this,C,ht).call(this);const r=this.history.filter(i=>i.type==="step").length;n+=`<agent_state>
`,n+=`<user_request>
`,n+=`${this.task}
`,n+=`</user_request>
`,n+=`<step_info>
`,n+=`Step ${r+1} of ${this.config.maxSteps} max possible steps
`,n+=`Current time: ${new Date().toLocaleString()}
`,n+=`</step_info>
`,n+=`</agent_state>

`,n+=`<agent_history>
`;let o=0;for(const i of this.history)i.type==="step"?(o++,n+=`<step_${o}>
`,n+=`Evaluation of Previous Step: ${i.reflection.evaluation_previous_goal}
`,n+=`Memory: ${i.reflection.memory}
`,n+=`Next Goal: ${i.reflection.next_goal}
`,n+=`Action Results: ${i.action.output}
`,n+=`</step_${o}>
`):i.type==="observation"?n+=`<sys>${i.content}</sys>
`:i.type==="user_takeover"?n+=`<sys>User took over control and made changes to the page</sys>
`:i.type;n+=`</agent_history>

`;let s=t.content;return this.config.transformPageContent&&(s=await this.config.transformPageContent(s)),n+=`<browser_state>
`,n+=t.header+`
`,n+=s+`
`,n+=t.footer+`

`,n+=`</browser_state>

`,n},at);function isHTMLElement(e){return!!e&&e.nodeType===1}function isInputElement(e){return(e==null?void 0:e.nodeType)===1&&e.tagName==="INPUT"}function isTextAreaElement(e){return(e==null?void 0:e.nodeType)===1&&e.tagName==="TEXTAREA"}function isSelectElement(e){return(e==null?void 0:e.nodeType)===1&&e.tagName==="SELECT"}function isAnchorElement(e){return(e==null?void 0:e.nodeType)===1&&e.tagName==="A"}function getIframeOffset(e){var r;const t=(r=e.ownerDocument.defaultView)==null?void 0:r.frameElement;if(!t)return{x:0,y:0};const n=t.getBoundingClientRect();return{x:n.left,y:n.top}}function getNativeValueSetter(e){return Object.getOwnPropertyDescriptor(Object.getPrototypeOf(e),"value").set}async function waitFor(e){await new Promise(t=>setTimeout(t,e*1e3))}async function movePointerToElement(e,t,n){const r=getIframeOffset(e);window.dispatchEvent(new CustomEvent("PageAgent::MovePointerTo",{detail:{x:t+r.x,y:n+r.y}})),await waitFor(.3)}async function clickPointer(){window.dispatchEvent(new CustomEvent("PageAgent::ClickPointer"))}async function enablePassThrough(){window.dispatchEvent(new CustomEvent("PageAgent::EnablePassThrough"))}async function disablePassThrough(){window.dispatchEvent(new CustomEvent("PageAgent::DisablePassThrough"))}function getElementByIndex(e,t){const n=e.get(t);if(!n)throw new Error(`No interactive element found at index ${t}`);const r=n.ref;if(!r)throw new Error(`Element at index ${t} does not have a reference`);if(!isHTMLElement(r))throw new Error(`Element at index ${t} is not an HTMLElement`);return r}var lastClickedElement=null;function blurLastClickedElement(){lastClickedElement&&(lastClickedElement.dispatchEvent(new PointerEvent("pointerout",{bubbles:!0})),lastClickedElement.dispatchEvent(new PointerEvent("pointerleave",{bubbles:!1})),lastClickedElement.dispatchEvent(new MouseEvent("mouseout",{bubbles:!0})),lastClickedElement.dispatchEvent(new MouseEvent("mouseleave",{bubbles:!1})),lastClickedElement.blur(),lastClickedElement=null)}async function clickElement(e){var u;blurLastClickedElement(),lastClickedElement=e,await scrollIntoViewIfNeeded(e);const t=(u=e.ownerDocument.defaultView)==null?void 0:u.frameElement;t&&await scrollIntoViewIfNeeded(t);const n=e.getBoundingClientRect(),r=n.left+n.width/2,o=n.top+n.height/2;await movePointerToElement(e,r,o),await clickPointer(),await waitFor(.1);const s=e.ownerDocument;await enablePassThrough();const i=s.elementFromPoint(r,o);await disablePassThrough();const a=i instanceof HTMLElement&&e.contains(i)?i:e,l={bubbles:!0,cancelable:!0,clientX:r,clientY:o,pointerType:"mouse"},d={bubbles:!0,cancelable:!0,clientX:r,clientY:o,button:0};a.dispatchEvent(new PointerEvent("pointerover",l)),a.dispatchEvent(new PointerEvent("pointerenter",{...l,bubbles:!1})),a.dispatchEvent(new MouseEvent("mouseover",d)),a.dispatchEvent(new MouseEvent("mouseenter",{...d,bubbles:!1})),a.dispatchEvent(new PointerEvent("pointerdown",l)),a.dispatchEvent(new MouseEvent("mousedown",d)),e.focus({preventScroll:!0}),a.dispatchEvent(new PointerEvent("pointerup",l)),a.dispatchEvent(new MouseEvent("mouseup",d)),a.click(),await waitFor(.2)}async function inputTextElement(e,t){const n=e.isContentEditable;if(!isInputElement(e)&&!isTextAreaElement(e)&&!n)throw new Error("Element is not an input, textarea, or contenteditable");if(await clickElement(e),n){if(e.dispatchEvent(new InputEvent("beforeinput",{bubbles:!0,cancelable:!0,inputType:"deleteContent"}))&&(e.innerText="",e.dispatchEvent(new InputEvent("input",{bubbles:!0,inputType:"deleteContent"}))),e.dispatchEvent(new InputEvent("beforeinput",{bubbles:!0,cancelable:!0,inputType:"insertText",data:t}))&&(e.innerText=t,e.dispatchEvent(new InputEvent("input",{bubbles:!0,inputType:"insertText",data:t}))),e.innerText.trim()!==t.trim()){e.focus();const r=e.ownerDocument,o=(r.defaultView||window).getSelection(),s=r.createRange();s.selectNodeContents(e),o==null||o.removeAllRanges(),o==null||o.addRange(s),r.execCommand("delete",!1),r.execCommand("insertText",!1,t)}e.dispatchEvent(new Event("change",{bubbles:!0})),e.blur()}else getNativeValueSetter(e).call(e,t);n||e.dispatchEvent(new Event("input",{bubbles:!0})),await waitFor(.1),blurLastClickedElement()}async function selectOptionElement(e,t){if(!isSelectElement(e))throw new Error("Element is not a select element");const n=Array.from(e.options).find(r=>{var o;return((o=r.textContent)==null?void 0:o.trim())===t.trim()});if(!n)throw new Error(`Option with text "${t}" not found in select element`);e.value=n.value,e.dispatchEvent(new Event("change",{bubbles:!0})),await waitFor(.1)}async function scrollIntoViewIfNeeded(e){const t=e;typeof t.scrollIntoViewIfNeeded=="function"?t.scrollIntoViewIfNeeded():e.scrollIntoView({behavior:"auto",block:"center",inline:"nearest"})}async function scrollVertically(e,t){if(t){const i=t;let a=i,l=!1,d=null,u=0,g=0;const h=e;for(;a&&g<10;){const m=window.getComputedStyle(a),y=/(auto|scroll|overlay)/.test(m.overflowY)||m.scrollbarWidth&&m.scrollbarWidth!=="auto"||m.scrollbarGutter&&m.scrollbarGutter!=="auto",v=a.scrollHeight>a.clientHeight;if(y&&v){const T=a.scrollTop,k=a.scrollHeight-a.clientHeight;let S=h/3;S>0?S=Math.min(S,k-T):S=Math.max(S,-T),a.scrollTop=T+S;const D=a.scrollTop-T;if(Math.abs(D)>.5){l=!0,d=a,u=D;break}}if(a===document.body||a===document.documentElement)break;a=a.parentElement,g++}return l?`Scrolled container (${d==null?void 0:d.tagName}) by ${u}px`:`No scrollable container found for element (${i.tagName})`}const n=e,r=i=>i.clientHeight>=window.innerHeight*.5,o=i=>!!(i&&/(auto|scroll|overlay)/.test(getComputedStyle(i).overflowY)&&i.scrollHeight>i.clientHeight&&r(i));let s=document.activeElement;for(;s&&!o(s)&&s!==document.body;)s=s.parentElement;if(s=o(s)?s:Array.from(document.querySelectorAll("*")).find(o)||document.scrollingElement||document.documentElement,s===document.scrollingElement||s===document.documentElement||s===document.body){const i=window.scrollY,a=document.documentElement.scrollHeight-window.innerHeight;window.scrollBy(0,n);const l=window.scrollY,d=l-i;if(Math.abs(d)<1)return n>0?"⚠️ Already at the bottom of the page, cannot scroll down further.":"⚠️ Already at the top of the page, cannot scroll up further.";const u=n>0&&l>=a-1,g=n<0&&l<=1;return u?`✅ Scrolled page by ${d}px. Reached the bottom of the page.`:g?`✅ Scrolled page by ${d}px. Reached the top of the page.`:`✅ Scrolled page by ${d}px.`}else{const i="The document is not scrollable. Falling back to container scroll.";console.log(`[PageController] ${i}`);const a=s.scrollTop,l=s.scrollHeight-s.clientHeight;s.scrollBy({top:n,behavior:"smooth"}),await waitFor(.1);const d=s.scrollTop,u=d-a;if(Math.abs(u)<1)return n>0?`⚠️ ${i} Already at the bottom of container (${s.tagName}), cannot scroll down further.`:`⚠️ ${i} Already at the top of container (${s.tagName}), cannot scroll up further.`;const g=n>0&&d>=l-1,h=n<0&&d<=1;return g?`✅ ${i} Scrolled container (${s.tagName}) by ${u}px. Reached the bottom.`:h?`✅ ${i} Scrolled container (${s.tagName}) by ${u}px. Reached the top.`:`✅ ${i} Scrolled container (${s.tagName}) by ${u}px.`}}async function scrollHorizontally(e,t){if(t){const i=t;let a=i,l=!1,d=null,u=0,g=0;const h=e;for(;a&&g<10;){const m=window.getComputedStyle(a),y=/(auto|scroll|overlay)/.test(m.overflowX)||m.scrollbarWidth&&m.scrollbarWidth!=="auto"||m.scrollbarGutter&&m.scrollbarGutter!=="auto",v=a.scrollWidth>a.clientWidth;if(y&&v){const T=a.scrollLeft,k=a.scrollWidth-a.clientWidth;let S=h/3;S>0?S=Math.min(S,k-T):S=Math.max(S,-T),a.scrollLeft=T+S;const D=a.scrollLeft-T;if(Math.abs(D)>.5){l=!0,d=a,u=D;break}}if(a===document.body||a===document.documentElement)break;a=a.parentElement,g++}return l?`Scrolled container (${d==null?void 0:d.tagName}) horizontally by ${u}px`:`No horizontally scrollable container found for element (${i.tagName})`}const n=e,r=i=>i.clientWidth>=window.innerWidth*.5,o=i=>!!(i&&/(auto|scroll|overlay)/.test(getComputedStyle(i).overflowX)&&i.scrollWidth>i.clientWidth&&r(i));let s=document.activeElement;for(;s&&!o(s)&&s!==document.body;)s=s.parentElement;if(s=o(s)?s:Array.from(document.querySelectorAll("*")).find(o)||document.scrollingElement||document.documentElement,s===document.scrollingElement||s===document.documentElement||s===document.body){const i=window.scrollX,a=document.documentElement.scrollWidth-window.innerWidth;window.scrollBy(n,0);const l=window.scrollX,d=l-i;if(Math.abs(d)<1)return n>0?"⚠️ Already at the right edge of the page, cannot scroll right further.":"⚠️ Already at the left edge of the page, cannot scroll left further.";const u=n>0&&l>=a-1,g=n<0&&l<=1;return u?`✅ Scrolled page by ${d}px. Reached the right edge of the page.`:g?`✅ Scrolled page by ${d}px. Reached the left edge of the page.`:`✅ Scrolled page horizontally by ${d}px.`}else{const i="The document is not scrollable. Falling back to container scroll.";console.log(`[PageController] ${i}`);const a=s.scrollLeft,l=s.scrollWidth-s.clientWidth;s.scrollBy({left:n,behavior:"smooth"}),await waitFor(.1);const d=s.scrollLeft,u=d-a;if(Math.abs(u)<1)return n>0?`⚠️ ${i} Already at the right edge of container (${s.tagName}), cannot scroll right further.`:`⚠️ ${i} Already at the left edge of container (${s.tagName}), cannot scroll left further.`;const g=n>0&&d>=l-1,h=n<0&&d<=1;return g?`✅ ${i} Scrolled container (${s.tagName}) by ${u}px. Reached the right edge.`:h?`✅ ${i} Scrolled container (${s.tagName}) by ${u}px. Reached the left edge.`:`✅ ${i} Scrolled container (${s.tagName}) horizontally by ${u}px.`}}var dom_tree_default=(e={doHighlightElements:!0,focusHighlightIndex:-1,viewportExpansion:0,debugMode:!1,interactiveBlacklist:[],interactiveWhitelist:[],highlightOpacity:.1,highlightLabelOpacity:.5})=>{const{interactiveBlacklist:t,interactiveWhitelist:n,highlightOpacity:r,highlightLabelOpacity:o}=e,{doHighlightElements:s,focusHighlightIndex:i,viewportExpansion:a,debugMode:l}=e;let d=0;const u=new WeakMap;function g(c,f){!c||c.nodeType!==Node.ELEMENT_NODE||u.set(c,{...u.get(c),...f})}const h={boundingRects:new WeakMap,clientRects:new WeakMap,computedStyles:new WeakMap,clearCache:()=>{h.boundingRects=new WeakMap,h.clientRects=new WeakMap,h.computedStyles=new WeakMap}};function m(c){if(!c)return null;if(h.boundingRects.has(c))return h.boundingRects.get(c);const f=c.getBoundingClientRect();return f&&h.boundingRects.set(c,f),f}function y(c){if(!c)return null;if(h.computedStyles.has(c))return h.computedStyles.get(c);const f=window.getComputedStyle(c);return f&&h.computedStyles.set(c,f),f}function v(c){if(!c)return null;if(h.clientRects.has(c))return h.clientRects.get(c);const f=c.getClientRects();return f&&h.clientRects.set(c,f),f}const T={},k={current:0},S="playwright-highlight-container";function D(c,f,$=null){if(!c)return f;const _=[];let x=null,N=20,E=16,B=null;try{let Z=document.getElementById(S);Z||(Z=document.createElement("div"),Z.id=S,Z.style.position="fixed",Z.style.pointerEvents="none",Z.style.top="0",Z.style.left="0",Z.style.width="100%",Z.style.height="100%",Z.style.zIndex="2147483640",Z.style.backgroundColor="transparent",document.body.appendChild(Z));const F=c.getClientRects();if(!F||F.length===0)return f;const ne=["#FF0000","#00FF00","#0000FF","#FFA500","#800080","#008080","#FF69B4","#4B0082","#FF4500","#2E8B57","#DC143C","#4682B4"];let re=ne[f%ne.length];const he=re+Math.floor(r*255).toString(16).padStart(2,"0");re=re+Math.floor(o*255).toString(16).padStart(2,"0");let z={x:0,y:0};if($){const q=$.getBoundingClientRect();z.x=q.left,z.y=q.top}const P=document.createDocumentFragment();for(const q of F){if(q.width===0||q.height===0)continue;const G=document.createElement("div");G.style.position="fixed",G.style.border=`2px solid ${re}`,G.style.backgroundColor=he,G.style.pointerEvents="none",G.style.boxSizing="border-box";const j=q.top+z.y,pe=q.left+z.x;G.style.top=`${j}px`,G.style.left=`${pe}px`,G.style.width=`${q.width}px`,G.style.height=`${q.height}px`,P.appendChild(G),_.push({element:G,initialRect:q})}const A=F[0];x=document.createElement("div"),x.className="playwright-highlight-label",x.style.position="fixed",x.style.background=re,x.style.color="white",x.style.padding="1px 4px",x.style.borderRadius="4px",x.style.fontSize=`${Math.min(12,Math.max(8,A.height/2))}px`,x.textContent=f.toString(),N=x.offsetWidth>0?x.offsetWidth:N,E=x.offsetHeight>0?x.offsetHeight:E;const K=A.top+z.y,Le=A.left+z.x;let Ge=K+2,Oe=Le+A.width-N-2;(A.width<N+4||A.height<E+4)&&(Ge=K-E-2,Oe=Le+A.width-N,Oe<z.x&&(Oe=Le)),Ge=Math.max(0,Math.min(Ge,window.innerHeight-E)),Oe=Math.max(0,Math.min(Oe,window.innerWidth-N)),x.style.top=`${Ge}px`,x.style.left=`${Oe}px`,P.appendChild(x);const Ye=((q,G)=>{let j=0;return(...pe)=>{const oe=performance.now();if(!(oe-j<G))return j=oe,q(...pe)}})(()=>{const q=c.getClientRects();let G={x:0,y:0};if($){const j=$.getBoundingClientRect();G.x=j.left,G.y=j.top}if(_.forEach((j,pe)=>{if(pe<q.length){const oe=q[pe],Ae=oe.top+G.y,ve=oe.left+G.x;j.element.style.top=`${Ae}px`,j.element.style.left=`${ve}px`,j.element.style.width=`${oe.width}px`,j.element.style.height=`${oe.height}px`,j.element.style.display=oe.width===0||oe.height===0?"none":"block"}else j.element.style.display="none"}),q.length<_.length)for(let j=q.length;j<_.length;j++)_[j].element.style.display="none";if(x&&q.length>0){const j=q[0],pe=j.top+G.y,oe=j.left+G.x;let Ae=pe+2,ve=oe+j.width-N-2;(j.width<N+4||j.height<E+4)&&(Ae=pe-E-2,ve=oe+j.width-N,ve<G.x&&(ve=oe)),Ae=Math.max(0,Math.min(Ae,window.innerHeight-E)),ve=Math.max(0,Math.min(ve,window.innerWidth-N)),x.style.top=`${Ae}px`,x.style.left=`${ve}px`,x.style.display="block"}else x&&(x.style.display="none")},16);return window.addEventListener("scroll",Ye,!0),window.addEventListener("resize",Ye),B=()=>{window.removeEventListener("scroll",Ye,!0),window.removeEventListener("resize",Ye),_.forEach(q=>q.element.remove()),x&&x.remove()},Z.appendChild(P),f+1}finally{B&&(window._highlightCleanupFunctions=window._highlightCleanupFunctions||[]).push(B)}}function U(c){if(!c||c.nodeType!==Node.ELEMENT_NODE)return null;const f=y(c);if(!f)return null;const $=f.display;if($==="inline"||$==="inline-block")return null;const _=f.overflowX,x=f.overflowY,N=f.scrollbarWidth&&f.scrollbarWidth!=="auto"||f.scrollbarGutter&&f.scrollbarGutter!=="auto",E=_==="auto"||_==="scroll",B=x==="auto"||x==="scroll";if(!E&&!B&&!N)return null;const Z=c.scrollWidth-c.clientWidth,F=c.scrollHeight-c.clientHeight,ne=4;if(Z<ne&&F<ne||!B&&!N&&Z<ne||!E&&!N&&F<ne)return null;const re=c.scrollTop,he=c.scrollLeft,z={top:re,right:c.scrollWidth-c.clientWidth-c.scrollLeft,bottom:c.scrollHeight-c.clientHeight-c.scrollTop,left:he};return g(c,{scrollable:!0,scrollData:z}),z}function Y(c){try{if(a===-1){const E=c.parentElement;if(!E)return!1;try{return E.checkVisibility({checkOpacity:!0,checkVisibilityCSS:!0})}catch{const Z=window.getComputedStyle(E);return Z.display!=="none"&&Z.visibility!=="hidden"&&Z.opacity!=="0"}}const f=document.createRange();f.selectNodeContents(c);const $=f.getClientRects();if(!$||$.length===0)return!1;let _=!1,x=!1;for(const E of $)if(E.width>0&&E.height>0&&(_=!0,!(E.bottom<-a||E.top>window.innerHeight+a||E.right<-a||E.left>window.innerWidth+a))){x=!0;break}if(!_||!x)return!1;const N=c.parentElement;if(!N)return!1;try{return N.checkVisibility({checkOpacity:!0,checkVisibilityCSS:!0})}catch{const B=window.getComputedStyle(N);return B.display!=="none"&&B.visibility!=="hidden"&&B.opacity!=="0"}}catch(f){return console.warn("Error checking text node visibility:",f),!1}}function O(c){if(!c||!c.tagName)return!1;const f=new Set(["body","div","main","article","section","nav","header","footer"]),$=c.tagName.toLowerCase();return f.has($)?!0:!new Set(["svg","script","style","link","meta","noscript","template"]).has($)}function M(c){const f=y(c);return c.offsetWidth>0&&c.offsetHeight>0&&(f==null?void 0:f.visibility)!=="hidden"&&(f==null?void 0:f.display)!=="none"}function V(c){var re,he;if(!c||c.nodeType!==Node.ELEMENT_NODE||t.includes(c))return!1;if(n.includes(c))return!0;const f=c.tagName.toLowerCase(),$=y(c),_=new Set(["pointer","move","text","grab","grabbing","cell","copy","alias","all-scroll","col-resize","context-menu","crosshair","e-resize","ew-resize","help","n-resize","ne-resize","nesw-resize","ns-resize","nw-resize","nwse-resize","row-resize","s-resize","se-resize","sw-resize","vertical-text","w-resize","zoom-in","zoom-out"]),x=new Set(["not-allowed","no-drop","wait","progress","initial","inherit"]);function N(z){return z.tagName.toLowerCase()==="html"?!1:!!($!=null&&$.cursor&&_.has($.cursor))}if(N(c))return!0;const E=new Set(["a","button","input","select","textarea","details","summary","label","option","optgroup","fieldset","legend"]),B=new Set(["disabled","readonly"]);if(E.has(f)){if($!=null&&$.cursor&&x.has($.cursor))return!1;for(const z of B)if(c.hasAttribute(z)||c.getAttribute(z)==="true"||c.getAttribute(z)==="")return!1;return!(c.disabled||c.readOnly||c.inert)}const Z=c.getAttribute("role"),F=c.getAttribute("aria-role");if(c.getAttribute("contenteditable")==="true"||c.isContentEditable||c.classList&&(c.classList.contains("button")||c.classList.contains("dropdown-toggle")||c.getAttribute("data-index")||c.getAttribute("data-toggle")==="dropdown"||c.getAttribute("aria-haspopup")==="true"))return!0;const ne=new Set(["button","menu","menubar","menuitem","menuitemradio","menuitemcheckbox","radio","checkbox","tab","switch","slider","spinbutton","combobox","searchbox","textbox","listbox","option","scrollbar"]);if(E.has(f)||Z&&ne.has(Z)||F&&ne.has(F))return!0;try{if(typeof getEventListeners=="function"){const P=getEventListeners(c);for(const A of["click","mousedown","mouseup","dblclick"])if(P[A]&&P[A].length>0)return!0}const z=((he=(re=c==null?void 0:c.ownerDocument)==null?void 0:re.defaultView)==null?void 0:he.getEventListenersForNode)||window.getEventListenersForNode;if(typeof z=="function"){const P=z(c);for(const A of["click","mousedown","mouseup","keydown","keyup","submit","change","input","focus","blur"])for(const K of P)if(K.type===A)return!0}for(const P of["onclick","onmousedown","onmouseup","ondblclick"])if(c.hasAttribute(P)||typeof c[P]=="function")return!0}catch{}return!!U(c)}function X(c){if(a===-1)return!0;const f=v(c);if(!f||f.length===0)return!1;let $=!1;for(const E of f)if(E.width>0&&E.height>0&&!(E.bottom<-a||E.top>window.innerHeight+a||E.right<-a||E.left>window.innerWidth+a)){$=!0;break}if(!$)return!1;if(c.ownerDocument!==window.document)return!0;let _=Array.from(f).find(E=>E.width>0&&E.height>0);if(!_)return!1;const x=c.getRootNode();if(x instanceof ShadowRoot){const E=_.left+_.width/2,B=_.top+_.height/2;try{const Z=x.elementFromPoint(E,B);if(!Z)return!1;let F=Z;for(;F&&F!==x;){if(F===c)return!0;F=F.parentElement}return!1}catch{return!0}}const N=5;return[{x:_.left+_.width/2,y:_.top+_.height/2},{x:_.left+N,y:_.top+N},{x:_.right-N,y:_.bottom-N}].some(({x:E,y:B})=>{try{const Z=document.elementFromPoint(E,B);if(!Z)return!1;let F=Z;for(;F&&F!==document.documentElement;){if(F===c)return!0;F=F.parentElement}return!1}catch{return!0}})}function Q(c,f){if(f===-1)return!0;const $=c.getClientRects();if(!$||$.length===0){const _=m(c);return!_||_.width===0||_.height===0?!1:!(_.bottom<-f||_.top>window.innerHeight+f||_.right<-f||_.left>window.innerWidth+f)}for(const _ of $)if(!(_.width===0||_.height===0)&&!(_.bottom<-f||_.top>window.innerHeight+f||_.right<-f||_.left>window.innerWidth+f))return!0;return!1}const W=["aria-expanded","aria-checked","aria-selected","aria-pressed","aria-haspopup","aria-controls","aria-owns","aria-activedescendant","aria-valuenow","aria-valuetext","aria-valuemax","aria-valuemin","aria-autocomplete"];function ye(c){for(let f=0;f<W.length;f++)if(c.hasAttribute(W[f]))return!0;return!1}function te(c){if(!c||c.nodeType!==Node.ELEMENT_NODE)return!1;const f=c.tagName.toLowerCase();return new Set(["a","button","input","select","textarea","details","summary","label"]).has(f)?!0:c.hasAttribute("onclick")||c.hasAttribute("role")||c.hasAttribute("tabindex")||ye(c)||c.hasAttribute("data-action")||c.getAttribute("contenteditable")==="true"}const ue=new Set(["a","button","input","select","textarea","summary","details","label","option","li"]),ce=new Set(["button","link","menuitem","menuitemradio","menuitemcheckbox","radio","checkbox","tab","switch","slider","spinbutton","combobox","searchbox","textbox","listbox","listitem","treeitem","row","option","scrollbar"]);function we(c){if(!c||c.nodeType!==Node.ELEMENT_NODE||!M(c))return!1;const f=c.hasAttribute("role")||c.hasAttribute("tabindex")||c.hasAttribute("onclick")||typeof c.onclick=="function",$=/\b(btn|clickable|menu|item|entry|link)\b/i.test(c.className||""),_=!!c.closest('button,a,[role="button"],.menu,.dropdown,.list,.toolbar'),x=[...c.children].some(M),N=c.parentElement&&c.parentElement.isSameNode(document.body);return(V(c)||f||$)&&x&&_&&!N}function He(c){var _,x,N;if(!c||c.nodeType!==Node.ELEMENT_NODE)return!1;const f=c.tagName.toLowerCase(),$=c.getAttribute("role");if(f==="iframe"||ue.has(f)||$&&ce.has($)||c.isContentEditable||c.getAttribute("contenteditable")==="true"||c.hasAttribute("data-testid")||c.hasAttribute("data-cy")||c.hasAttribute("data-test")||c.hasAttribute("onclick")||typeof c.onclick=="function"||ye(c))return!0;try{const E=((x=(_=c==null?void 0:c.ownerDocument)==null?void 0:_.defaultView)==null?void 0:x.getEventListenersForNode)||window.getEventListenersForNode;if(typeof E=="function"){const B=E(c);for(const Z of["click","mousedown","mouseup","keydown","keyup","submit","change","input","focus","blur"])for(const F of B)if(F.type===Z)return!0}if(["onmousedown","onmouseup","onkeydown","onkeyup","onsubmit","onchange","oninput","onfocus","onblur"].some(B=>c.hasAttribute(B)))return!0}catch{}return!!(we(c)||(N=u.get(c))!=null&&N.scrollable)}function Je(c,f,$,_){if(!c.isInteractive)return!1;let x=!1;return _?He(f)?x=!0:x=!1:x=!0,x&&(c.isInViewport=Q(f,a),(c.isInViewport||a===-1)&&(c.highlightIndex=d++,s))?(i>=0?i===c.highlightIndex&&D(f,c.highlightIndex,$):D(f,c.highlightIndex,$),!0):!1}function de(c,f=null,$=!1){var E,B,Z,F,ne,re,he;if(!c||c.id===S||c.nodeType!==Node.ELEMENT_NODE&&c.nodeType!==Node.TEXT_NODE||!c||c.id===S||((E=c.dataset)==null?void 0:E.browserUseIgnore)==="true"||((B=c.dataset)==null?void 0:B.pageAgentIgnore)==="true"||c.getAttribute&&c.getAttribute("aria-hidden")==="true")return null;if(c===document.body){const z={tagName:"body",attributes:{},xpath:"/body",children:[]};for(const A of c.childNodes){const K=de(A,f,!1);K&&z.children.push(K)}const P=`${k.current++}`;return T[P]=z,P}if(c.nodeType!==Node.ELEMENT_NODE&&c.nodeType!==Node.TEXT_NODE)return null;if(c.nodeType===Node.TEXT_NODE){const z=(Z=c.textContent)==null?void 0:Z.trim();if(!z)return null;const P=c.parentElement;if(!P||P.tagName.toLowerCase()==="script")return null;const A=`${k.current++}`;return T[A]={type:"TEXT_NODE",text:z,isVisible:Y(c)},A}if(c.nodeType===Node.ELEMENT_NODE&&!O(c))return null;if(a!==-1&&!c.shadowRoot){const z=m(c),P=y(c),A=P&&(P.position==="fixed"||P.position==="sticky"),K=c.offsetWidth>0||c.offsetHeight>0;if(!z||!A&&!K&&(z.bottom<-a||z.top>window.innerHeight+a||z.right<-a||z.left>window.innerWidth+a))return null}const _={tagName:c.tagName.toLowerCase(),attributes:{},children:[]};if(te(c)||c.tagName.toLowerCase()==="iframe"||c.tagName.toLowerCase()==="body"){const z=((F=c.getAttributeNames)==null?void 0:F.call(c))||[];for(const P of z){const A=c.getAttribute(P);_.attributes[P]=A}c.tagName.toLowerCase()==="input"&&(c.type==="checkbox"||c.type==="radio")&&(_.attributes.checked=c.checked?"true":"false")}let x=!1;if(c.nodeType===Node.ELEMENT_NODE&&(_.isVisible=M(c),_.isVisible)){_.isTopElement=X(c);const z=c.getAttribute("role"),P=z==="menu"||z==="menubar"||z==="listbox";if((_.isTopElement||P)&&(_.isInteractive=V(c),x=Je(_,c,f,$),_.ref=c,_.isInteractive&&Object.keys(_.attributes).length===0)){const A=((ne=c.getAttributeNames)==null?void 0:ne.call(c))||[];for(const K of A){const Le=c.getAttribute(K);_.attributes[K]=Le}}}if(c.tagName){const z=c.tagName.toLowerCase();if(z==="iframe")try{const P=c.contentDocument||((re=c.contentWindow)==null?void 0:re.document);if(P)for(const A of P.childNodes){const K=de(A,c,!1);K&&_.children.push(K)}}catch(P){console.warn("Unable to access iframe:",P)}else if(c.isContentEditable||c.getAttribute("contenteditable")==="true"||c.id==="tinymce"||c.classList.contains("mce-content-body")||z==="body"&&((he=c.getAttribute("data-id"))!=null&&he.startsWith("mce_")))for(const P of c.childNodes){const A=de(P,f,x);A&&_.children.push(A)}else{if(c.shadowRoot){_.shadowRoot=!0;for(const P of c.shadowRoot.childNodes){const A=de(P,f,x);A&&_.children.push(A)}}for(const P of c.childNodes){const A=de(P,f,x||$);A&&_.children.push(A)}}}if(_.tagName==="a"&&_.children.length===0&&!_.attributes.href){const z=m(c);if(!(z&&z.width>0&&z.height>0||c.offsetWidth>0||c.offsetHeight>0))return null}_.extra=u.get(c)||null;const N=`${k.current++}`;return T[N]=_,N}const qe=de(document.body);return h.clearCache(),{rootId:qe,map:T}},DEFAULT_VIEWPORT_EXPANSION=-1;function resolveViewportExpansion(e){return e??DEFAULT_VIEWPORT_EXPANSION}var SEMANTIC_TAGS=new Set(["nav","menu","header","footer","aside","dialog"]),newElementsCache=new WeakMap;function getFlatTree(e){const t=resolveViewportExpansion(e.viewportExpansion),n=[];for(const i of e.interactiveBlacklist||[])typeof i=="function"?n.push(i()):n.push(i);const r=[];for(const i of e.interactiveWhitelist||[])typeof i=="function"?r.push(i()):r.push(i);const o=dom_tree_default({doHighlightElements:!0,debugMode:!0,focusHighlightIndex:-1,viewportExpansion:t,interactiveBlacklist:n,interactiveWhitelist:r,highlightOpacity:e.highlightOpacity??0,highlightLabelOpacity:e.highlightLabelOpacity??.1}),s=window.location.href;for(const i in o.map){const a=o.map[i];if(a.isInteractive&&a.ref){const l=a.ref;newElementsCache.has(l)||(newElementsCache.set(l,s),a.isNew=!0)}}return o}var globRegexCache=new Map;function globToRegex(e){let t=globRegexCache.get(e);if(!t){const n=e.replace(/[.+^${}()|[\]\\]/g,"\\$&");t=new RegExp(`^${n.replace(/\*/g,".*")}$`),globRegexCache.set(e,t)}return t}function matchAttributes(e,t){const n={};for(const r of t)if(r.includes("*")){const o=globToRegex(r);for(const s of Object.keys(e))o.test(s)&&e[s].trim()&&(n[s]=e[s].trim())}else{const o=e[r];o&&o.trim()&&(n[r]=o.trim())}return n}function flatTreeToString(e,t=[],n=!1){const r=["title","type","checked","name","role","value","placeholder","data-date-format","alt","aria-label","aria-expanded","data-state","aria-checked","id","for","target","aria-haspopup","aria-controls","aria-owns","contenteditable"],o=[...t,...r],s=(h,m)=>h.length>m?h.substring(0,m)+"...":h,i=h=>{const m=e.map[h];if(!m)return null;if(m.type==="TEXT_NODE"){const y=m;return{type:"text",text:y.text,isVisible:y.isVisible,parent:null,children:[]}}else{const y=m,v=[];if(y.children)for(const T of y.children){const k=i(T);k&&(k.parent=null,v.push(k))}return{type:"element",tagName:y.tagName,attributes:y.attributes??{},isVisible:y.isVisible??!1,isInteractive:y.isInteractive??!1,isTopElement:y.isTopElement??!1,isNew:y.isNew??!1,highlightIndex:y.highlightIndex,parent:null,children:v,extra:y.extra??{}}}},a=(h,m=null)=>{h.parent=m;for(const y of h.children)a(y,h)},l=i(e.rootId);if(!l)return"";a(l);const d=h=>{let m=h.parent;for(;m;){if(m.type==="element"&&m.highlightIndex!==void 0)return!0;m=m.parent}return!1},u=(h,m,y)=>{var k,S,D,U;let v=m;const T="	".repeat(m);if(h.type==="element"){const Y=n&&h.tagName&&SEMANTIC_TAGS.has(h.tagName);if(h.highlightIndex!==void 0){v+=1;const V=getAllTextTillNextClickableElement(h);let X="";if(o.length>0&&h.attributes){const W=matchAttributes(h.attributes,o),ye=Object.keys(W);if(ye.length>1){const te=new Set,ue={};for(const ce of ye){const we=W[ce];we.length>5&&(we in ue?te.add(ce):ue[we]=ce)}for(const ce of te)delete W[ce]}W.role===h.tagName&&delete W.role;for(const te of["aria-label","placeholder","title"])W[te]&&W[te].toLowerCase().trim()===V.toLowerCase().trim()&&delete W[te];Object.keys(W).length>0&&(X=Object.entries(W).map(([te,ue])=>`${te}=${s(ue,20)}`).join(" "))}let Q=`${T}${h.isNew?`*[${h.highlightIndex}]`:`[${h.highlightIndex}]`}<${h.tagName??""}`;if(X&&(Q+=` ${X}`),h.extra&&h.extra.scrollable){let W="";(k=h.extra.scrollData)!=null&&k.left&&(W+=`left=${h.extra.scrollData.left}, `),(S=h.extra.scrollData)!=null&&S.top&&(W+=`top=${h.extra.scrollData.top}, `),(D=h.extra.scrollData)!=null&&D.right&&(W+=`right=${h.extra.scrollData.right}, `),(U=h.extra.scrollData)!=null&&U.bottom&&(W+=`bottom=${h.extra.scrollData.bottom}`),Q+=` data-scrollable="${W}"`}if(V){const W=V.trim();X||(Q+=" "),Q+=`>${W}`}else X||(Q+=" ");Q+=" />",y.push(Q)}const O=Y&&h.highlightIndex===void 0,M=O?y.length:-1;O&&(y.push(`${T}<${h.tagName}>`),v+=1);for(const V of h.children)u(V,v,y);O&&(y.length===M+1?y.pop():y.push(`${T}</${h.tagName}>`))}else if(h.type==="text"){if(d(h))return;h.parent&&h.parent.type==="element"&&h.parent.isVisible&&h.parent.isTopElement&&y.push(`${T}${h.text??""}`)}},g=[];return u(l,0,g),g.join(`
`)}var getAllTextTillNextClickableElement=(e,t=-1)=>{const n=[],r=(o,s)=>{if(!(t!==-1&&s>t)&&!(o.type==="element"&&o!==e&&o.highlightIndex!==void 0)){if(o.type==="text"&&o.text)n.push(o.text);else if(o.type==="element")for(const i of o.children)r(i,s+1)}};return r(e,0),n.join(`
`).trim()};function getSelectorMap(e){const t=new Map,n=Object.keys(e.map);for(const r of n){const o=e.map[r];o.isInteractive&&typeof o.highlightIndex=="number"&&t.set(o.highlightIndex,o)}return t}function getElementTextMap(e){const t=e.split(`
`).map(r=>r.trim()).filter(r=>r.length>0),n=new Map;for(const r of t){const o=/^\[(\d+)\]<[^>]+>([^<]*)/.exec(r);if(o){const s=parseInt(o[1],10);n.set(s,r)}}return n}function cleanUpHighlights(){const e=window._highlightCleanupFunctions||[];for(const t of e)typeof t=="function"&&t();window._highlightCleanupFunctions=[]}window.addEventListener("popstate",()=>{cleanUpHighlights()});window.addEventListener("hashchange",()=>{cleanUpHighlights()});window.addEventListener("beforeunload",()=>{cleanUpHighlights()});var navigation=window.navigation;if(navigation&&typeof navigation.addEventListener=="function")navigation.addEventListener("navigate",()=>{cleanUpHighlights()});else{let e=window.location.href;setInterval(()=>{window.location.href!==e&&(e=window.location.href,cleanUpHighlights())},500)}function getPageInfo(){const e=window.innerWidth,t=window.innerHeight,n=Math.max(document.documentElement.scrollWidth,document.body.scrollWidth||0),r=Math.max(document.documentElement.scrollHeight,document.body.scrollHeight||0),o=window.scrollX||window.pageXOffset||document.documentElement.scrollLeft||0,s=window.scrollY||window.pageYOffset||document.documentElement.scrollTop||0,i=Math.max(0,r-(window.innerHeight+s)),a=Math.max(0,n-(window.innerWidth+o));return{viewport_width:e,viewport_height:t,page_width:n,page_height:r,scroll_x:o,scroll_y:s,pixels_above:s,pixels_below:i,pages_above:t>0?s/t:0,pages_below:t>0?i/t:0,total_pages:t>0?r/t:0,current_page_position:s/Math.max(1,r-t),pixels_left:o,pixels_right:a}}function patchReact(e){const t=document.querySelectorAll('[data-reactroot], [data-reactid], [data-react-checksum], #root, #app, [id^="root-"], [id^="app-"], #adex-wrapper, #adex-root');for(const n of t)n.setAttribute("data-page-agent-not-interactive","true")}var PageController=class extends EventTarget{constructor(t={}){super();R(this,"config");R(this,"flatTree",null);R(this,"selectorMap",new Map);R(this,"elementTextMap",new Map);R(this,"simplifiedHTML","<EMPTY>");R(this,"lastTimeUpdate",0);R(this,"isIndexed",!1);R(this,"mask",null);R(this,"maskReady",null);this.config=t,patchReact(),t.enableMask&&this.initMask()}initMask(){this.maskReady===null&&(this.maskReady=(async()=>{const{SimulatorMask:t}=await __vitePreload(async()=>{const{SimulatorMask:n}=await import("./SimulatorMask-BHVXyogh-C8gwQx9z.js");return{SimulatorMask:n}},[]);this.mask=new t})())}async getCurrentUrl(){return window.location.href}async getLastUpdateTime(){return this.lastTimeUpdate}async getBrowserState(){const t=window.location.href,n=document.title,r=getPageInfo(),o=resolveViewportExpansion(this.config.viewportExpansion);await this.updateTree();const s=this.simplifiedHTML;return{url:t,title:n,header:`${`Current Page: [${n}](${t})`}
${`Page info: ${r.viewport_width}x${r.viewport_height}px viewport, ${r.page_width}x${r.page_height}px total page size, ${r.pages_above.toFixed(1)} pages above, ${r.pages_below.toFixed(1)} pages below, ${r.total_pages.toFixed(1)} total pages, at ${(r.current_page_position*100).toFixed(0)}% of page`}

${o===-1?"Interactive elements from top layer of the current page (full page):":"Interactive elements from top layer of the current page inside the viewport:"}

${r.pixels_above>4&&o!==-1?`... ${r.pixels_above} pixels above (${r.pages_above.toFixed(1)} pages) - scroll to see more ...`:"[Start of page]"}`,content:s,footer:r.pixels_below>4&&o!==-1?`... ${r.pixels_below} pixels below (${r.pages_below.toFixed(1)} pages) - scroll to see more ...`:"[End of page]"}}async updateTree(){this.dispatchEvent(new Event("beforeUpdate")),this.lastTimeUpdate=Date.now(),this.mask&&(this.mask.wrapper.style.pointerEvents="none"),cleanUpHighlights();const t=[...this.config.interactiveBlacklist||[],...Array.from(document.querySelectorAll("[data-page-agent-not-interactive]"))];return this.flatTree=getFlatTree({...this.config,interactiveBlacklist:t}),this.simplifiedHTML=flatTreeToString(this.flatTree,this.config.includeAttributes,this.config.keepSemanticTags),this.selectorMap.clear(),this.selectorMap=getSelectorMap(this.flatTree),this.elementTextMap.clear(),this.elementTextMap=getElementTextMap(this.simplifiedHTML),this.isIndexed=!0,this.mask&&(this.mask.wrapper.style.pointerEvents="auto"),this.dispatchEvent(new Event("afterUpdate")),this.simplifiedHTML}async cleanUpHighlights(){console.log("[PageController] cleanUpHighlights"),cleanUpHighlights()}assertIndexed(){if(!this.isIndexed)throw new Error("DOM tree not indexed yet. Can not perform actions on elements.")}async clickElement(t){try{this.assertIndexed();const n=getElementByIndex(this.selectorMap,t),r=this.elementTextMap.get(t);return await clickElement(n),isAnchorElement(n)&&n.target==="_blank"?{success:!0,message:`✅ Clicked element (${r??t}). ⚠️ Link opened in a new tab.`}:{success:!0,message:`✅ Clicked element (${r??t}).`}}catch(n){return{success:!1,message:`❌ Failed to click element: ${n}`}}}async inputText(t,n){try{this.assertIndexed();const r=getElementByIndex(this.selectorMap,t),o=this.elementTextMap.get(t);return await inputTextElement(r,n),{success:!0,message:`✅ Input text (${n}) into element (${o??t}).`}}catch(r){return{success:!1,message:`❌ Failed to input text: ${r}`}}}async selectOption(t,n){try{this.assertIndexed();const r=getElementByIndex(this.selectorMap,t),o=this.elementTextMap.get(t);return await selectOptionElement(r,n),{success:!0,message:`✅ Selected option (${n}) in element (${o??t}).`}}catch(r){return{success:!1,message:`❌ Failed to select option: ${r}`}}}async scroll(t){try{const{down:n,numPages:r,pixels:o,index:s}=t;return this.assertIndexed(),{success:!0,message:await scrollVertically((o??r*window.innerHeight)*(n?1:-1),s!==void 0?getElementByIndex(this.selectorMap,s):null)}}catch(n){return{success:!1,message:`❌ Failed to scroll: ${n}`}}}async scrollHorizontally(t){try{const{right:n,pixels:r,index:o}=t;return this.assertIndexed(),{success:!0,message:await scrollHorizontally(r*(n?1:-1),o!==void 0?getElementByIndex(this.selectorMap,o):null)}}catch(n){return{success:!1,message:`❌ Failed to scroll horizontally: ${n}`}}}async executeJavascript(script,signal){try{const asyncFunction=eval(`(async (signal) => { ${script} })`),result=await asyncFunction(signal);return{success:!0,message:`✅ Executed JavaScript. Result: ${result}`}}catch(t){return{success:!1,message:`❌ Error executing JavaScript: ${t}`}}}async showMask(){var t;await this.maskReady,(t=this.mask)==null||t.show()}async hideMask(){var t;await this.maskReady,(t=this.mask)==null||t.hide()}dispose(){var t;cleanUpHighlights(),this.flatTree=null,this.selectorMap.clear(),this.elementTextMap.clear(),this.simplifiedHTML="<EMPTY>",this.isIndexed=!1,(t=this.mask)==null||t.dispose(),this.mask=null}};(function(){try{if(typeof document<"u"){var e=document.createElement("style");e.appendChild(document.createTextNode(`._wrapper_1tu05_1 {
	position: fixed;
	bottom: 100px;
	left: 50%;
	transform: translateX(-50%) translateY(20px);
	opacity: 0;
	z-index: 2147483642; /* 比 SimulatorMask 高一层 */
	box-sizing: border-box;

	overflow: visible;

	* {
		box-sizing: border-box;
	}

	--width: 360px;
	--height: 40px;
	--border-radius: 12px;

	--side-space: 12px; /* 控制栏两侧的间距 */
	--history-width: calc(var(--width) - var(--side-space) * 2);

	--color-1: rgb(57, 182, 255);
	--color-2: rgb(189, 69, 251);
	--color-3: rgb(255, 87, 51);
	--color-4: rgb(255, 214, 0);

	width: var(--width);
	height: var(--height);

	transition: all 0.3s ease-in-out;

	/* 响应式设计 */
	@media (max-width: 480px) {
		width: calc(100vw - 40px);
		--width: calc(100vw - 40px);
	}

	._background_1tu05_39 {
		position: absolute;
		inset: -2px -8px;
		border-radius: calc(var(--border-radius) + 4px);
		filter: blur(16px);
		overflow: hidden;
		/* mix-blend-mode: lighten; */
		/* display: none; */

		&::before {
			content: '';
			z-index: -1;
			pointer-events: none;
			position: absolute;
			width: 100%;
			height: 100%;
			/* left: -100%; */
			left: 0;
			top: 0;

			background-image: linear-gradient(
				to bottom left,
				var(--color-1),
				var(--color-2),
				var(--color-1)
			);
			animation: _mask-running_1tu05_1 2s linear infinite;
		}
		&::after {
			content: '';
			z-index: -1;
			pointer-events: none;
			position: absolute;
			width: 100%;
			height: 100%;
			left: 0;
			top: 0;

			background-image: linear-gradient(
				to bottom left,
				var(--color-2),
				var(--color-1),
				var(--color-2)
			);
			animation: _mask-running_1tu05_1 2s linear infinite;
			animation-delay: 1s;
		}
	}
}

@keyframes _mask-running_1tu05_1 {
	from {
		transform: translateX(-100%);
	}
	to {
		transform: translateX(100%);
	}
}

/* 控制栏 */
._header_1tu05_99 {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 8px 12px;
	user-select: none;

	position: absolute;
	inset: 0;

	cursor: pointer;
	flex-shrink: 0; /* 防止 header 被压缩 */

	background: rgba(0, 0, 0, 0.5);
	backdrop-filter: blur(10px);
	border-radius: var(--border-radius);
	background-clip: padding-box;

	box-shadow:
		0 0 0px 2px rgba(255, 255, 255, 0.4),
		0 0 5px 1px rgba(255, 255, 255, 0.3);

	._statusSection_1tu05_121 {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: 1;
		min-height: 24px; /* 确保垂直居中 */

		._indicator_1tu05_128 {
			width: 6px;
			height: 6px;
			border-radius: 50%;
			background: rgba(255, 255, 255, 0.5);
			flex-shrink: 0;
			animation: none; /* 默认无动画 */

			/* 运行状态 - 有动画 */
			&._thinking_1tu05_137 {
				background: rgb(57, 182, 255);
				animation: _pulse_1tu05_1 0.8s ease-in-out infinite;
			}

			&._tool_executing_1tu05_142 {
				background: rgb(189, 69, 251);
				animation: _pulse_1tu05_1 0.6s ease-in-out infinite;
			}

			&._retry_1tu05_147 {
				background: rgb(255, 214, 0);
				animation: _retryPulse_1tu05_1 1s ease-in-out infinite;
			}

			/* 静止状态 - 无动画 */
			&._completed_1tu05_153,
			&._input_1tu05_154,
			&._output_1tu05_155 {
				background: rgb(34, 197, 94);
				animation: none;
			}

			&._error_1tu05_160 {
				background: rgb(239, 68, 68);
				animation: none;
			}
		}

		._statusText_1tu05_166 {
			color: white;
			font-size: 12px;
			line-height: 1;
			font-weight: 500;
			transition: all 0.3s ease-in-out;
			position: relative;
			overflow: hidden;
			display: flex;
			align-items: center;
			min-height: 24px; /* 确保垂直居中 */

			&._fadeOut_1tu05_178 {
				animation: _statusTextFadeOut_1tu05_1 0.3s ease forwards;
			}

			&._fadeIn_1tu05_182 {
				animation: _statusTextFadeIn_1tu05_1 0.3s ease forwards;
			}
		}
	}

	._controls_1tu05_188 {
		display: flex;
		align-items: center;
		gap: 4px;

		._controlButton_1tu05_193 {
			width: 24px;
			height: 24px;
			border: none;
			border-radius: 4px;
			background: rgba(255, 255, 255, 0.1);
			color: white;
			cursor: pointer;
			display: flex;
			align-items: center;
			justify-content: center;
			font-size: 12px;
			line-height: 1;

			&:hover {
				background: rgba(255, 255, 255, 0.2);
			}
		}

		._stopButton_1tu05_212 {
			background: rgba(239, 68, 68, 0.2);
			color: rgb(255, 41, 41);
			font-weight: 600;

			&:hover {
				background: rgba(239, 68, 68, 0.3);
			}
		}
	}
}

@keyframes _statusTextFadeIn_1tu05_1 {
	0% {
		opacity: 0;
		transform: translateY(5px);
	}
	100% {
		opacity: 1;
		transform: translateY(0);
	}
}

@keyframes _statusTextFadeOut_1tu05_1 {
	0% {
		opacity: 1;
		transform: translateY(0);
	}
	100% {
		opacity: 0;
		transform: translateY(-5px);
	}
}

._historySectionWrapper_1tu05_246 {
	position: absolute;
	width: var(--history-width);
	bottom: var(--height);
	left: var(--side-space);
	z-index: -2;

	padding-top: 0px;
	visibility: collapse;
	overflow: hidden;

	transition: all 0.2s;

	background: rgba(2, 0, 20, 0.5);
	/* background: rgba(186, 186, 186, 0.2); */
	backdrop-filter: blur(10px);

	text-shadow: 0 0 1px rgba(0, 0, 0, 0.2);

	border-top-left-radius: calc(var(--border-radius) + 4px);
	border-top-right-radius: calc(var(--border-radius) + 4px);

	/* border: 2px solid rgba(255, 255, 255, 0.8); */
	border: 2px solid rgba(255, 255, 255, 0.4);
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.6);

	/* @media (prefers-color-scheme: dark) {
		box-shadow:
			0 8px 32px 0 rgba(0, 0, 0, 0.85),
			0 2px 12px 0 rgba(57, 182, 255, 0.1);
	} */

	._expanded_1tu05_278 & {
		padding-top: 8px;
		visibility: visible;
	}

	._historySection_1tu05_246 {
		position: relative;
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-width: none;
		max-height: 0;
		padding-inline: 8px;

		transition: max-height 0.2s;

		._expanded_1tu05_278 & {
			max-height: min(500px, calc(100vh - 200px - var(--height)));
		}

		._historyItem_1tu05_297 {
			/* backdrop-filter: blur(10px); */
			padding: 8px 10px;
			margin-bottom: 6px;
			background: linear-gradient(135deg, rgba(255, 255, 255, 0.08), rgba(255, 255, 255, 0.03));
			border-radius: 8px;
			border-left: 2px solid rgba(57, 182, 255, 0.5);
			font-size: 12px;
			color: white;
			/* color: black; */
			line-height: 1.3;
			position: relative;
			overflow: hidden;

			/* 微妙的内阴影 */
			box-shadow:
				inset 0 1px 0 rgba(255, 255, 255, 0.1),
				0 1px 3px rgba(0, 0, 0, 0.1);

			&::before {
				content: '';
				position: absolute;
				top: 0;
				left: 0;
				right: 0;
				height: 1px;
				background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.2), transparent);
			}

			&:hover {
				background: linear-gradient(135deg, rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0.06));
				/* transform: translateY(-1px); */
				box-shadow:
					inset 0 1px 0 rgba(255, 255, 255, 0.15),
					0 2px 4px rgba(0, 0, 0, 0.15);
			}

			&:last-child {
				margin-bottom: 10px;
			}

			&._completed_1tu05_153,
			&._input_1tu05_154,
			&._output_1tu05_155 {
				border-left-color: rgb(34, 197, 94);
				background: linear-gradient(135deg, rgba(34, 197, 94, 0.1), rgba(34, 197, 94, 0.05));
			}

			&._error_1tu05_160 {
				border-left-color: rgb(239, 68, 68);
				background: linear-gradient(135deg, rgba(239, 68, 68, 0.1), rgba(239, 68, 68, 0.05));
			}

			&._retry_1tu05_147 {
				border-left-color: rgb(255, 214, 0);
				background: linear-gradient(135deg, rgba(255, 214, 0, 0.1), rgba(255, 214, 0, 0.05));
			}

			&._observation_1tu05_355 {
				border-left-color: rgb(147, 51, 234);
				background: linear-gradient(135deg, rgba(147, 51, 234, 0.1), rgba(147, 51, 234, 0.05));
			}

			&._question_1tu05_360 {
				border-left-color: rgb(255, 159, 67);
				background: linear-gradient(135deg, rgba(255, 159, 67, 0.15), rgba(255, 159, 67, 0.08));
			}

			/* 突出显示 done 成功结果 */
			&._doneSuccess_1tu05_366 {
				background: linear-gradient(
					135deg,
					rgba(34, 197, 94, 0.25),
					rgba(34, 197, 94, 0.15),
					rgba(34, 197, 94, 0.08)
				);
				border: none;
				border-left: 4px solid rgb(34, 197, 94);
				box-shadow:
					0 4px 12px rgba(34, 197, 94, 0.3),
					inset 0 1px 0 rgba(255, 255, 255, 0.2),
					0 0 20px rgba(34, 197, 94, 0.1);
				font-weight: 600;
				color: rgb(220, 252, 231);
				padding: 10px 12px;
				margin-bottom: 8px;
				border-radius: 8px;
				position: relative;
				overflow: hidden;

				&::before {
					background: linear-gradient(90deg, transparent, rgba(34, 197, 94, 0.4), transparent);
				}

				&::after {
					content: '';
					position: absolute;
					top: 0;
					left: -100%;
					width: 100%;
					height: 100%;
					background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.1), transparent);
					animation: _shimmer_1tu05_1 2s ease-in-out infinite;
				}

				._historyContent_1tu05_402 {
					._statusIcon_1tu05_403 {
						font-size: 16px;
						animation: _celebrate_1tu05_1 0.8s ease-in-out;
						filter: drop-shadow(0 2px 4px rgba(34, 197, 94, 0.5));
					}
				}
			}

			/* 突出显示 done 失败结果 */
			&._doneError_1tu05_412 {
				background: linear-gradient(
					135deg,
					rgba(239, 68, 68, 0.25),
					rgba(239, 68, 68, 0.15),
					rgba(239, 68, 68, 0.08)
				);
				border: none;
				border-left: 4px solid rgb(239, 68, 68);
				box-shadow:
					0 4px 12px rgba(239, 68, 68, 0.3),
					inset 0 1px 0 rgba(255, 255, 255, 0.2),
					0 0 20px rgba(239, 68, 68, 0.1);
				font-weight: 600;
				color: rgb(254, 226, 226);
				padding: 10px 12px;
				margin-bottom: 8px;
				border-radius: 8px;
				position: relative;
				overflow: hidden;

				&::before {
					background: linear-gradient(90deg, transparent, rgba(239, 68, 68, 0.4), transparent);
				}

				._historyContent_1tu05_402 {
					._statusIcon_1tu05_403 {
						font-size: 16px;
						filter: drop-shadow(0 2px 4px rgba(239, 68, 68, 0.5));
					}
				}
			}

			._historyContent_1tu05_402 {
				display: flex;
				align-items: flex-start;
				gap: 8px;

				word-break: break-all;
				white-space: pre-wrap;

				/* overflow-x: auto; */

				._statusIcon_1tu05_403 {
					font-size: 12px;
					flex-shrink: 0;
					line-height: 1;
					transition: all 0.3s ease;
				}

				._reflectionLines_1tu05_462 {
					display: flex;
					flex-direction: column;
					gap: 4px;
				}
			}

			._historyMeta_1tu05_469 {
				font-size: 10px;
				color: rgba(255, 255, 255, 0.6);
				/* color: rgb(61, 61, 61); */
				margin-top: 8px;
				line-height: 1;
			}
		}
	}
}

/* 动画关键帧 - 更快的闪烁 */
@keyframes _pulse_1tu05_1 {
	0%,
	100% {
		opacity: 1;
		transform: scale(1);
	}
	50% {
		opacity: 0.4;
		transform: scale(1.3);
	}
}

/* 重试动画 - 旋转脉冲 */
@keyframes _retryPulse_1tu05_1 {
	0%,
	100% {
		opacity: 1;
		transform: scale(1) rotate(0deg);
	}
	25% {
		opacity: 0.6;
		transform: scale(1.2) rotate(90deg);
	}
	50% {
		opacity: 0.8;
		transform: scale(1.1) rotate(180deg);
	}
	75% {
		opacity: 0.6;
		transform: scale(1.2) rotate(270deg);
	}
}

/* 庆祝动画 */
@keyframes _celebrate_1tu05_1 {
	0%,
	100% {
		transform: scale(1);
	}
	25% {
		transform: scale(1.2) rotate(-5deg);
	}
	75% {
		transform: scale(1.2) rotate(5deg);
	}
}

/* done 卡片的光泽效果 */
@keyframes _shimmer_1tu05_1 {
	0% {
		left: -100%;
	}
	100% {
		left: 100%;
	}
}

/* 输入区域样式 */
._inputSectionWrapper_1tu05_539 {
	position: absolute;
	width: var(--history-width);
	top: var(--height);
	left: var(--side-space);
	z-index: -1;

	visibility: visible;
	overflow: hidden;

	height: 48px;

	transition: all 0.2s;

	background: rgba(186, 186, 186, 0.2);
	backdrop-filter: blur(10px);

	border-bottom-left-radius: calc(var(--border-radius) + 4px);
	border-bottom-right-radius: calc(var(--border-radius) + 4px);

	border: 2px solid rgba(255, 255, 255, 0.3);
	box-shadow: 0 1px 16px rgba(0, 0, 0, 0.4);

	&._hidden_1tu05_562 {
		visibility: collapse;
		height: 0;
	}

	._inputSection_1tu05_539 {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 8px 8px;

		._taskInput_1tu05_573 {
			flex: 1;
			background: rgba(255, 255, 255, 0.4);
			border: 1px solid rgba(255, 255, 255, 0.3);
			border-radius: 10px;
			padding-inline: 10px;
			color: rgb(20, 20, 20);
			font-size: 12px;
			height: 28px;
			line-height: 1;
			outline: none;
			transition: all 0.2s ease;

			/* text-shadow: 0 0 2px rgba(255, 255, 255, 0.8); */

			/* border-color: rgba(57, 182, 255, 0.3); */

			&::placeholder {
				color: rgb(53, 53, 53);
			}

			&:focus {
				background: rgba(255, 255, 255, 0.8);
				border-color: rgba(57, 182, 255, 0.6);
				box-shadow: 0 0 0 2px rgba(57, 182, 255, 0.2);
			}
		}
	}
}`)),document.head.appendChild(e)}}catch(t){console.error("vite-plugin-css-injected-by-js",t)}})();(function(){try{if(typeof document<"u"){var e=document.createElement("style");e.appendChild(document.createTextNode(`._wrapper_1tu05_1 {
	position: fixed;
	bottom: 100px;
	left: 50%;
	transform: translateX(-50%) translateY(20px);
	opacity: 0;
	z-index: 2147483642; /* 比 SimulatorMask 高一层 */
	box-sizing: border-box;

	overflow: visible;

	* {
		box-sizing: border-box;
	}

	--width: 360px;
	--height: 40px;
	--border-radius: 12px;

	--side-space: 12px; /* 控制栏两侧的间距 */
	--history-width: calc(var(--width) - var(--side-space) * 2);

	--color-1: rgb(57, 182, 255);
	--color-2: rgb(189, 69, 251);
	--color-3: rgb(255, 87, 51);
	--color-4: rgb(255, 214, 0);

	width: var(--width);
	height: var(--height);

	transition: all 0.3s ease-in-out;

	/* 响应式设计 */
	@media (max-width: 480px) {
		width: calc(100vw - 40px);
		--width: calc(100vw - 40px);
	}

	._background_1tu05_39 {
		position: absolute;
		inset: -2px -8px;
		border-radius: calc(var(--border-radius) + 4px);
		filter: blur(16px);
		overflow: hidden;
		/* mix-blend-mode: lighten; */
		/* display: none; */

		&::before {
			content: '';
			z-index: -1;
			pointer-events: none;
			position: absolute;
			width: 100%;
			height: 100%;
			/* left: -100%; */
			left: 0;
			top: 0;

			background-image: linear-gradient(
				to bottom left,
				var(--color-1),
				var(--color-2),
				var(--color-1)
			);
			animation: _mask-running_1tu05_1 2s linear infinite;
		}
		&::after {
			content: '';
			z-index: -1;
			pointer-events: none;
			position: absolute;
			width: 100%;
			height: 100%;
			left: 0;
			top: 0;

			background-image: linear-gradient(
				to bottom left,
				var(--color-2),
				var(--color-1),
				var(--color-2)
			);
			animation: _mask-running_1tu05_1 2s linear infinite;
			animation-delay: 1s;
		}
	}
}

@keyframes _mask-running_1tu05_1 {
	from {
		transform: translateX(-100%);
	}
	to {
		transform: translateX(100%);
	}
}

/* 控制栏 */
._header_1tu05_99 {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 8px 12px;
	user-select: none;

	position: absolute;
	inset: 0;

	cursor: pointer;
	flex-shrink: 0; /* 防止 header 被压缩 */

	background: rgba(0, 0, 0, 0.5);
	backdrop-filter: blur(10px);
	border-radius: var(--border-radius);
	background-clip: padding-box;

	box-shadow:
		0 0 0px 2px rgba(255, 255, 255, 0.4),
		0 0 5px 1px rgba(255, 255, 255, 0.3);

	._statusSection_1tu05_121 {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: 1;
		min-height: 24px; /* 确保垂直居中 */

		._indicator_1tu05_128 {
			width: 6px;
			height: 6px;
			border-radius: 50%;
			background: rgba(255, 255, 255, 0.5);
			flex-shrink: 0;
			animation: none; /* 默认无动画 */

			/* 运行状态 - 有动画 */
			&._thinking_1tu05_137 {
				background: rgb(57, 182, 255);
				animation: _pulse_1tu05_1 0.8s ease-in-out infinite;
			}

			&._tool_executing_1tu05_142 {
				background: rgb(189, 69, 251);
				animation: _pulse_1tu05_1 0.6s ease-in-out infinite;
			}

			&._retry_1tu05_147 {
				background: rgb(255, 214, 0);
				animation: _retryPulse_1tu05_1 1s ease-in-out infinite;
			}

			/* 静止状态 - 无动画 */
			&._completed_1tu05_153,
			&._input_1tu05_154,
			&._output_1tu05_155 {
				background: rgb(34, 197, 94);
				animation: none;
			}

			&._error_1tu05_160 {
				background: rgb(239, 68, 68);
				animation: none;
			}
		}

		._statusText_1tu05_166 {
			color: white;
			font-size: 12px;
			line-height: 1;
			font-weight: 500;
			transition: all 0.3s ease-in-out;
			position: relative;
			overflow: hidden;
			display: flex;
			align-items: center;
			min-height: 24px; /* 确保垂直居中 */

			&._fadeOut_1tu05_178 {
				animation: _statusTextFadeOut_1tu05_1 0.3s ease forwards;
			}

			&._fadeIn_1tu05_182 {
				animation: _statusTextFadeIn_1tu05_1 0.3s ease forwards;
			}
		}
	}

	._controls_1tu05_188 {
		display: flex;
		align-items: center;
		gap: 4px;

		._controlButton_1tu05_193 {
			width: 24px;
			height: 24px;
			border: none;
			border-radius: 4px;
			background: rgba(255, 255, 255, 0.1);
			color: white;
			cursor: pointer;
			display: flex;
			align-items: center;
			justify-content: center;
			font-size: 12px;
			line-height: 1;

			&:hover {
				background: rgba(255, 255, 255, 0.2);
			}
		}

		._stopButton_1tu05_212 {
			background: rgba(239, 68, 68, 0.2);
			color: rgb(255, 41, 41);
			font-weight: 600;

			&:hover {
				background: rgba(239, 68, 68, 0.3);
			}
		}
	}
}

@keyframes _statusTextFadeIn_1tu05_1 {
	0% {
		opacity: 0;
		transform: translateY(5px);
	}
	100% {
		opacity: 1;
		transform: translateY(0);
	}
}

@keyframes _statusTextFadeOut_1tu05_1 {
	0% {
		opacity: 1;
		transform: translateY(0);
	}
	100% {
		opacity: 0;
		transform: translateY(-5px);
	}
}

._historySectionWrapper_1tu05_246 {
	position: absolute;
	width: var(--history-width);
	bottom: var(--height);
	left: var(--side-space);
	z-index: -2;

	padding-top: 0px;
	visibility: collapse;
	overflow: hidden;

	transition: all 0.2s;

	background: rgba(2, 0, 20, 0.5);
	/* background: rgba(186, 186, 186, 0.2); */
	backdrop-filter: blur(10px);

	text-shadow: 0 0 1px rgba(0, 0, 0, 0.2);

	border-top-left-radius: calc(var(--border-radius) + 4px);
	border-top-right-radius: calc(var(--border-radius) + 4px);

	/* border: 2px solid rgba(255, 255, 255, 0.8); */
	border: 2px solid rgba(255, 255, 255, 0.4);
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.6);

	/* @media (prefers-color-scheme: dark) {
		box-shadow:
			0 8px 32px 0 rgba(0, 0, 0, 0.85),
			0 2px 12px 0 rgba(57, 182, 255, 0.1);
	} */

	._expanded_1tu05_278 & {
		padding-top: 8px;
		visibility: visible;
	}

	._historySection_1tu05_246 {
		position: relative;
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-width: none;
		max-height: 0;
		padding-inline: 8px;

		transition: max-height 0.2s;

		._expanded_1tu05_278 & {
			max-height: min(500px, calc(100vh - 200px - var(--height)));
		}

		._historyItem_1tu05_297 {
			/* backdrop-filter: blur(10px); */
			padding: 8px 10px;
			margin-bottom: 6px;
			background: linear-gradient(135deg, rgba(255, 255, 255, 0.08), rgba(255, 255, 255, 0.03));
			border-radius: 8px;
			border-left: 2px solid rgba(57, 182, 255, 0.5);
			font-size: 12px;
			color: white;
			/* color: black; */
			line-height: 1.3;
			position: relative;
			overflow: hidden;

			/* 微妙的内阴影 */
			box-shadow:
				inset 0 1px 0 rgba(255, 255, 255, 0.1),
				0 1px 3px rgba(0, 0, 0, 0.1);

			&::before {
				content: '';
				position: absolute;
				top: 0;
				left: 0;
				right: 0;
				height: 1px;
				background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.2), transparent);
			}

			&:hover {
				background: linear-gradient(135deg, rgba(255, 255, 255, 0.12), rgba(255, 255, 255, 0.06));
				/* transform: translateY(-1px); */
				box-shadow:
					inset 0 1px 0 rgba(255, 255, 255, 0.15),
					0 2px 4px rgba(0, 0, 0, 0.15);
			}

			&:last-child {
				margin-bottom: 10px;
			}

			&._completed_1tu05_153,
			&._input_1tu05_154,
			&._output_1tu05_155 {
				border-left-color: rgb(34, 197, 94);
				background: linear-gradient(135deg, rgba(34, 197, 94, 0.1), rgba(34, 197, 94, 0.05));
			}

			&._error_1tu05_160 {
				border-left-color: rgb(239, 68, 68);
				background: linear-gradient(135deg, rgba(239, 68, 68, 0.1), rgba(239, 68, 68, 0.05));
			}

			&._retry_1tu05_147 {
				border-left-color: rgb(255, 214, 0);
				background: linear-gradient(135deg, rgba(255, 214, 0, 0.1), rgba(255, 214, 0, 0.05));
			}

			&._observation_1tu05_355 {
				border-left-color: rgb(147, 51, 234);
				background: linear-gradient(135deg, rgba(147, 51, 234, 0.1), rgba(147, 51, 234, 0.05));
			}

			&._question_1tu05_360 {
				border-left-color: rgb(255, 159, 67);
				background: linear-gradient(135deg, rgba(255, 159, 67, 0.15), rgba(255, 159, 67, 0.08));
			}

			/* 突出显示 done 成功结果 */
			&._doneSuccess_1tu05_366 {
				background: linear-gradient(
					135deg,
					rgba(34, 197, 94, 0.25),
					rgba(34, 197, 94, 0.15),
					rgba(34, 197, 94, 0.08)
				);
				border: none;
				border-left: 4px solid rgb(34, 197, 94);
				box-shadow:
					0 4px 12px rgba(34, 197, 94, 0.3),
					inset 0 1px 0 rgba(255, 255, 255, 0.2),
					0 0 20px rgba(34, 197, 94, 0.1);
				font-weight: 600;
				color: rgb(220, 252, 231);
				padding: 10px 12px;
				margin-bottom: 8px;
				border-radius: 8px;
				position: relative;
				overflow: hidden;

				&::before {
					background: linear-gradient(90deg, transparent, rgba(34, 197, 94, 0.4), transparent);
				}

				&::after {
					content: '';
					position: absolute;
					top: 0;
					left: -100%;
					width: 100%;
					height: 100%;
					background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.1), transparent);
					animation: _shimmer_1tu05_1 2s ease-in-out infinite;
				}

				._historyContent_1tu05_402 {
					._statusIcon_1tu05_403 {
						font-size: 16px;
						animation: _celebrate_1tu05_1 0.8s ease-in-out;
						filter: drop-shadow(0 2px 4px rgba(34, 197, 94, 0.5));
					}
				}
			}

			/* 突出显示 done 失败结果 */
			&._doneError_1tu05_412 {
				background: linear-gradient(
					135deg,
					rgba(239, 68, 68, 0.25),
					rgba(239, 68, 68, 0.15),
					rgba(239, 68, 68, 0.08)
				);
				border: none;
				border-left: 4px solid rgb(239, 68, 68);
				box-shadow:
					0 4px 12px rgba(239, 68, 68, 0.3),
					inset 0 1px 0 rgba(255, 255, 255, 0.2),
					0 0 20px rgba(239, 68, 68, 0.1);
				font-weight: 600;
				color: rgb(254, 226, 226);
				padding: 10px 12px;
				margin-bottom: 8px;
				border-radius: 8px;
				position: relative;
				overflow: hidden;

				&::before {
					background: linear-gradient(90deg, transparent, rgba(239, 68, 68, 0.4), transparent);
				}

				._historyContent_1tu05_402 {
					._statusIcon_1tu05_403 {
						font-size: 16px;
						filter: drop-shadow(0 2px 4px rgba(239, 68, 68, 0.5));
					}
				}
			}

			._historyContent_1tu05_402 {
				display: flex;
				align-items: flex-start;
				gap: 8px;

				word-break: break-all;
				white-space: pre-wrap;

				/* overflow-x: auto; */

				._statusIcon_1tu05_403 {
					font-size: 12px;
					flex-shrink: 0;
					line-height: 1;
					transition: all 0.3s ease;
				}

				._reflectionLines_1tu05_462 {
					display: flex;
					flex-direction: column;
					gap: 4px;
				}
			}

			._historyMeta_1tu05_469 {
				font-size: 10px;
				color: rgba(255, 255, 255, 0.6);
				/* color: rgb(61, 61, 61); */
				margin-top: 8px;
				line-height: 1;
			}
		}
	}
}

/* 动画关键帧 - 更快的闪烁 */
@keyframes _pulse_1tu05_1 {
	0%,
	100% {
		opacity: 1;
		transform: scale(1);
	}
	50% {
		opacity: 0.4;
		transform: scale(1.3);
	}
}

/* 重试动画 - 旋转脉冲 */
@keyframes _retryPulse_1tu05_1 {
	0%,
	100% {
		opacity: 1;
		transform: scale(1) rotate(0deg);
	}
	25% {
		opacity: 0.6;
		transform: scale(1.2) rotate(90deg);
	}
	50% {
		opacity: 0.8;
		transform: scale(1.1) rotate(180deg);
	}
	75% {
		opacity: 0.6;
		transform: scale(1.2) rotate(270deg);
	}
}

/* 庆祝动画 */
@keyframes _celebrate_1tu05_1 {
	0%,
	100% {
		transform: scale(1);
	}
	25% {
		transform: scale(1.2) rotate(-5deg);
	}
	75% {
		transform: scale(1.2) rotate(5deg);
	}
}

/* done 卡片的光泽效果 */
@keyframes _shimmer_1tu05_1 {
	0% {
		left: -100%;
	}
	100% {
		left: 100%;
	}
}

/* 输入区域样式 */
._inputSectionWrapper_1tu05_539 {
	position: absolute;
	width: var(--history-width);
	top: var(--height);
	left: var(--side-space);
	z-index: -1;

	visibility: visible;
	overflow: hidden;

	height: 48px;

	transition: all 0.2s;

	background: rgba(186, 186, 186, 0.2);
	backdrop-filter: blur(10px);

	border-bottom-left-radius: calc(var(--border-radius) + 4px);
	border-bottom-right-radius: calc(var(--border-radius) + 4px);

	border: 2px solid rgba(255, 255, 255, 0.3);
	box-shadow: 0 1px 16px rgba(0, 0, 0, 0.4);

	&._hidden_1tu05_562 {
		visibility: collapse;
		height: 0;
	}

	._inputSection_1tu05_539 {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 8px 8px;

		._taskInput_1tu05_573 {
			flex: 1;
			background: rgba(255, 255, 255, 0.4);
			border: 1px solid rgba(255, 255, 255, 0.3);
			border-radius: 10px;
			padding-inline: 10px;
			color: rgb(20, 20, 20);
			font-size: 12px;
			height: 28px;
			line-height: 1;
			outline: none;
			transition: all 0.2s ease;

			/* text-shadow: 0 0 2px rgba(255, 255, 255, 0.8); */

			/* border-color: rgba(57, 182, 255, 0.3); */

			&::placeholder {
				color: rgb(53, 53, 53);
			}

			&:focus {
				background: rgba(255, 255, 255, 0.8);
				border-color: rgba(57, 182, 255, 0.6);
				box-shadow: 0 0 0 2px rgba(57, 182, 255, 0.2);
			}
		}
	}
}`)),document.head.appendChild(e)}}catch(t){console.error("vite-plugin-css-injected-by-js",t)}})();var locales={"en-US":{ui:{panel:{ready:"Ready",thinking:"Thinking...",taskInput:"Enter new task, describe steps in detail, press Enter to submit",userAnswerPrompt:"Please answer the question above, press Enter to submit",taskTerminated:"Task terminated",taskCompleted:"Task completed",userAnswer:"User answer: {{input}}",question:"Question: {{question}}",waitingPlaceholder:"Waiting for task to start...",stop:"Stop",close:"Close",expand:"Expand history",collapse:"Collapse history",step:"Step {{number}}"},tools:{clicking:"Clicking element [{{index}}]...",inputting:"Inputting text to element [{{index}}]...",selecting:'Selecting option "{{text}}"...',scrolling:"Scrolling page...",waiting:"Waiting {{seconds}} seconds...",askingUser:"Asking user...",done:"Task done",clicked:"🖱️ Clicked element [{{index}}]",inputted:'⌨️ Inputted text "{{text}}"',selected:'☑️ Selected option "{{text}}"',scrolled:"🛞 Page scrolled",waited:"⌛️ Wait completed",executing:"Executing {{toolName}}...",resultSuccess:"success",resultFailure:"failed",resultError:"error"},errors:{elementNotFound:"No interactive element found at index {{index}}",taskRequired:"Task description is required",executionFailed:"Task execution failed",notInputElement:"Element is not an input or textarea",notSelectElement:"Element is not a select element",optionNotFound:'Option "{{text}}" not found'}}},"zh-CN":{ui:{panel:{ready:"准备就绪",thinking:"正在思考...",taskInput:"输入新任务，详细描述步骤，回车提交",userAnswerPrompt:"请回答上面问题，回车提交",taskTerminated:"任务已终止",taskCompleted:"任务结束",userAnswer:"用户回答: {{input}}",question:"询问: {{question}}",waitingPlaceholder:"等待任务开始...",stop:"终止",close:"关闭",expand:"展开历史",collapse:"收起历史",step:"步骤 {{number}}"},tools:{clicking:"正在点击元素 [{{index}}]...",inputting:"正在输入文本到元素 [{{index}}]...",selecting:'正在选择选项 "{{text}}"...',scrolling:"正在滚动页面...",waiting:"等待 {{seconds}} 秒...",askingUser:"正在询问用户...",done:"结束任务",clicked:"🖱️ 已点击元素 [{{index}}]",inputted:'⌨️ 已输入文本 "{{text}}"',selected:'☑️ 已选择选项 "{{text}}"',scrolled:"🛞 页面滚动完成",waited:"⌛️ 等待完成",executing:"正在执行 {{toolName}}...",resultSuccess:"成功",resultFailure:"失败",resultError:"错误"},errors:{elementNotFound:"未找到索引为 {{index}} 的交互元素",taskRequired:"任务描述不能为空",executionFailed:"任务执行失败",notInputElement:"元素不是输入框或文本域",notSelectElement:"元素不是选择框",optionNotFound:'未找到选项 "{{text}}"'}}}},I18n=class{constructor(e="en-US"){R(this,"language");R(this,"translations");this.language=e in locales?e:"en-US",this.translations=locales[this.language]}t(e,t){const n=this.getNestedValue(this.translations,e);return n?t?this.interpolate(n,t):n:(console.warn(`Translation key "${e}" not found for language "${this.language}"`),e)}getNestedValue(e,t){return t.split(".").reduce((n,r)=>n==null?void 0:n[r],e)}interpolate(e,t){return e.replace(/\{\{(\w+)\}\}/g,(n,r)=>t[r]!=null?t[r].toString():n)}getLanguage(){return this.language}};function truncate(e,t){return e.length>t?e.substring(0,t)+"...":e}function escapeHtml(e){return e.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;").replace(/'/g,"&#039;")}var Panel_module_default={wrapper:"_wrapper_1tu05_1","mask-running":"_mask-running_1tu05_1",background:"_background_1tu05_39",header:"_header_1tu05_99",pulse:"_pulse_1tu05_1",retryPulse:"_retryPulse_1tu05_1",statusTextFadeOut:"_statusTextFadeOut_1tu05_1",statusTextFadeIn:"_statusTextFadeIn_1tu05_1",statusSection:"_statusSection_1tu05_121",indicator:"_indicator_1tu05_128",thinking:"_thinking_1tu05_137",tool_executing:"_tool_executing_1tu05_142",retry:"_retry_1tu05_147",completed:"_completed_1tu05_153",input:"_input_1tu05_154",output:"_output_1tu05_155",error:"_error_1tu05_160",statusText:"_statusText_1tu05_166",fadeOut:"_fadeOut_1tu05_178",fadeIn:"_fadeIn_1tu05_182",controls:"_controls_1tu05_188",controlButton:"_controlButton_1tu05_193",stopButton:"_stopButton_1tu05_212",historySectionWrapper:"_historySectionWrapper_1tu05_246",shimmer:"_shimmer_1tu05_1",celebrate:"_celebrate_1tu05_1",expanded:"_expanded_1tu05_278",historySection:"_historySection_1tu05_246",historyItem:"_historyItem_1tu05_297",observation:"_observation_1tu05_355",question:"_question_1tu05_360",doneSuccess:"_doneSuccess_1tu05_366",historyContent:"_historyContent_1tu05_402",statusIcon:"_statusIcon_1tu05_403",doneError:"_doneError_1tu05_412",reflectionLines:"_reflectionLines_1tu05_462",historyMeta:"_historyMeta_1tu05_469",inputSectionWrapper:"_inputSectionWrapper_1tu05_539",hidden:"_hidden_1tu05_562",inputSection:"_inputSection_1tu05_539",taskInput:"_taskInput_1tu05_573"};function createCard({icon:e,content:t,meta:n,type:r}){const o=r?Panel_module_default[r]:"",s=Array.isArray(t)?`<div class="${Panel_module_default.reflectionLines}">${t.map(i=>`<span>${escapeHtml(i)}</span>`).join("")}</div>`:`<span>${escapeHtml(t)}</span>`;return`
		<div class="${Panel_module_default.historyItem} ${o}">
			<div class="${Panel_module_default.historyContent}">
				<span class="${Panel_module_default.statusIcon}">${e}</span>
				${s}
			</div>
			${n?`<div class="${Panel_module_default.historyMeta}">${n}</div>`:""}
		</div>
	`}function createReflectionLines(e){const t=[];return e.evaluation_previous_goal&&t.push(`🔍 ${e.evaluation_previous_goal}`),e.memory&&t.push(`💾 ${e.memory}`),e.next_goal&&t.push(`🎯 ${e.next_goal}`),t}var ie,Ce,ae,ge,Ie,me,Pe,_e,H,je,Ee,J,be,le,Ze,se,Re,Ue,We,Be,Ve,b,gt,mt,_t,bt,et,tt,yt,wt,vt,Me,nt,xt,kt,$t,rt,Fe,Xe,Et,Tt,St,zt,Se,ot,st,It,Pt,Zt,ct,Panel=(ct=class{constructor(e,t={}){L(this,b);L(this,ie);L(this,Ce);L(this,ae);L(this,ge);L(this,Ie);L(this,me);L(this,Pe);L(this,_e);L(this,H);L(this,je);L(this,Ee,!1);L(this,J);L(this,be,null);L(this,le,!1);L(this,Ze,null);L(this,se,null);L(this,Re,!1);L(this,Ue,()=>w(this,b,gt).call(this));L(this,We,()=>w(this,b,mt).call(this));L(this,Be,e=>w(this,b,_t).call(this,e.detail));L(this,Ve,()=>this.dispose());I(this,H,e),I(this,je,t),I(this,J,new I18n(t.language??"en-US")),p(this,H).onAskUser=(n,r)=>w(this,b,bt).call(this,n,r==null?void 0:r.signal),I(this,ie,w(this,b,kt).call(this)),I(this,Ce,p(this,ie).querySelector(`.${Panel_module_default.indicator}`)),I(this,ae,p(this,ie).querySelector(`.${Panel_module_default.statusText}`)),I(this,ge,p(this,ie).querySelector(`.${Panel_module_default.historySection}`)),I(this,Ie,p(this,ie).querySelector(`.${Panel_module_default.expandButton}`)),I(this,me,p(this,ie).querySelector(`.${Panel_module_default.stopButton}`)),I(this,Pe,p(this,ie).querySelector(`.${Panel_module_default.inputSectionWrapper}`)),I(this,_e,p(this,ie).querySelector(`.${Panel_module_default.taskInput}`)),p(this,H).addEventListener("statuschange",p(this,Ue)),p(this,H).addEventListener("historychange",p(this,We)),p(this,H).addEventListener("activity",p(this,Be)),p(this,H).addEventListener("dispose",p(this,Ve)),w(this,b,$t).call(this),w(this,b,Et).call(this),w(this,b,Me).call(this),this.hide()}get wrapper(){return p(this,ie)}show(){this.wrapper.style.display="block",this.wrapper.offsetHeight,this.wrapper.style.opacity="1",this.wrapper.style.transform="translateX(-50%) translateY(0)"}hide(){this.wrapper.style.opacity="0",this.wrapper.style.transform="translateX(-50%) translateY(20px)",this.wrapper.style.display="none"}reset(){p(this,ae).textContent=p(this,J).t("ui.panel.ready"),w(this,b,Se).call(this,"thinking"),w(this,b,st).call(this),w(this,b,Xe).call(this),I(this,le,!1),I(this,be,null),w(this,b,Me).call(this)}expand(){w(this,b,Fe).call(this)}collapse(){w(this,b,Xe).call(this)}dispose(){p(this,H).removeEventListener("statuschange",p(this,Ue)),p(this,H).removeEventListener("historychange",p(this,We)),p(this,H).removeEventListener("activity",p(this,Be)),p(this,H).removeEventListener("dispose",p(this,Ve)),I(this,le,!1),w(this,b,Tt).call(this),this.wrapper.remove()}},ie=new WeakMap,Ce=new WeakMap,ae=new WeakMap,ge=new WeakMap,Ie=new WeakMap,me=new WeakMap,Pe=new WeakMap,_e=new WeakMap,H=new WeakMap,je=new WeakMap,Ee=new WeakMap,J=new WeakMap,be=new WeakMap,le=new WeakMap,Ze=new WeakMap,se=new WeakMap,Re=new WeakMap,Ue=new WeakMap,We=new WeakMap,Be=new WeakMap,Ve=new WeakMap,b=new WeakSet,gt=function(){var n;const e=p(this,H).status,t=e==="completed"&&((n=p(this,H).lastResult)==null?void 0:n.success)===!1;w(this,b,Se).call(this,t?"error":e),e==="running"?(p(this,me).textContent="■",p(this,me).title=p(this,J).t("ui.panel.stop")):(p(this,me).textContent="X",p(this,me).title=p(this,J).t("ui.panel.close")),e==="running"&&(this.show(),w(this,b,nt).call(this)),(e==="completed"||e==="error"||e==="stopped")&&(p(this,Ee)||w(this,b,Fe).call(this),w(this,b,xt).call(this)&&w(this,b,Me).call(this))},mt=function(){w(this,b,st).call(this)},_t=function(e){switch(e.type){case"thinking":I(this,se,p(this,J).t("ui.panel.thinking")),w(this,b,Se).call(this,"thinking");break;case"executing":I(this,se,w(this,b,tt).call(this,e.tool,e.input)),w(this,b,Se).call(this,"executing");break;case"executed":I(this,se,truncate(e.output,50));break;case"retrying":I(this,se,`Retrying (${e.attempt}/${e.maxAttempts})`),w(this,b,Se).call(this,"retrying");break;case"error":I(this,se,truncate(e.message,50)),w(this,b,Se).call(this,"error");break}},bt=function(e,t){return new Promise((n,r)=>{I(this,le,!0),I(this,be,n),p(this,Ee)||w(this,b,Fe).call(this);const o=document.createElement("div");o.innerHTML=createCard({icon:"❓",content:`Question: ${e}`,type:"question"});const s=o.firstElementChild;s.setAttribute("data-temp-card","true"),p(this,ge).appendChild(s),w(this,b,ot).call(this),w(this,b,Me).call(this,p(this,J).t("ui.panel.userAnswerPrompt")),t==null||t.addEventListener("abort",()=>{w(this,b,et).call(this),I(this,le,!1),I(this,be,null),r(t.reason)},{once:!0})})},et=function(){Array.from(p(this,ge).children).forEach(e=>{e.getAttribute("data-temp-card")==="true"&&e.remove()})},tt=function(e,t){const n=t;switch(e){case"click_element_by_index":return p(this,J).t("ui.tools.clicking",{index:n.index});case"input_text":return p(this,J).t("ui.tools.inputting",{index:n.index});case"select_dropdown_option":return p(this,J).t("ui.tools.selecting",{text:n.text});case"scroll":return p(this,J).t("ui.tools.scrolling");case"wait":return p(this,J).t("ui.tools.waiting",{seconds:n.seconds});case"ask_user":return p(this,J).t("ui.tools.askingUser");case"done":return p(this,J).t("ui.tools.done");default:return p(this,J).t("ui.tools.executing",{toolName:e})}},yt=function(){p(this,H).status==="running"?p(this,H).stop():p(this,H).dispose()},wt=function(){const e=p(this,_e).value.trim();e&&(w(this,b,nt).call(this),p(this,le)?w(this,b,vt).call(this,e):p(this,H).execute(e))},vt=function(e){w(this,b,et).call(this),I(this,le,!1),p(this,be)&&(p(this,be).call(this,e),I(this,be,null))},Me=function(e){p(this,_e).value="",p(this,_e).placeholder=e||p(this,J).t("ui.panel.taskInput"),p(this,Pe).classList.remove(Panel_module_default.hidden),setTimeout(()=>{p(this,_e).focus()},100)},nt=function(){p(this,Pe).classList.add(Panel_module_default.hidden)},xt=function(){if(p(this,le)||p(this,H).history.length===0)return!0;const e=p(this,H).status;return e==="completed"||e==="error"||e==="stopped"?p(this,je).promptForNextTask??!0:!1},kt=function(){const t=document.createElement("div");return t.id="page-agent-runtime_agent-panel",t.className=Panel_module_default.wrapper,t.setAttribute("data-browser-use-ignore","true"),t.setAttribute("data-page-agent-ignore","true"),t.innerHTML=`
			<div class="${Panel_module_default.background}"></div>
			<div class="${Panel_module_default.historySectionWrapper}">
				<div class="${Panel_module_default.historySection}">
					<div class="${Panel_module_default.historyItem}">
						<div class="${Panel_module_default.historyContent}">
							<span class="${Panel_module_default.statusIcon}">🧠</span>
							<span>${p(this,J).t("ui.panel.waitingPlaceholder")}</span>
						</div>
					</div>
				</div>
			</div>
			<div class="${Panel_module_default.header}">
				<div class="${Panel_module_default.statusSection}">
					<div class="${Panel_module_default.indicator} ${Panel_module_default.thinking}"></div>
					<div class="${Panel_module_default.statusText}">${p(this,J).t("ui.panel.ready")}</div>
				</div>
				<div class="${Panel_module_default.controls}">
					<button class="${Panel_module_default.controlButton} ${Panel_module_default.expandButton}" title="${p(this,J).t("ui.panel.expand")}">
						▼
					</button>
					<button class="${Panel_module_default.controlButton} ${Panel_module_default.stopButton}" title="${p(this,J).t("ui.panel.close")}">
						X
					</button>
				</div>
			</div>
			<div class="${Panel_module_default.inputSectionWrapper} ${Panel_module_default.hidden}">
				<div class="${Panel_module_default.inputSection}">
					<input 
						type="text" 
						class="${Panel_module_default.taskInput}" 
						maxlength="1000"
					/>
				</div>
			</div>
		`,document.body.appendChild(t),t},$t=function(){this.wrapper.querySelector(`.${Panel_module_default.header}`).addEventListener("click",e=>{e.target.closest(`.${Panel_module_default.controlButton}`)||w(this,b,rt).call(this)}),p(this,Ie).addEventListener("click",e=>{e.stopPropagation(),w(this,b,rt).call(this)}),p(this,me).addEventListener("click",e=>{e.stopPropagation(),w(this,b,yt).call(this)}),p(this,_e).addEventListener("keydown",e=>{e.isComposing||e.key==="Enter"&&(e.preventDefault(),w(this,b,wt).call(this))}),p(this,Pe).addEventListener("click",e=>{e.stopPropagation()})},rt=function(){p(this,Ee)?w(this,b,Xe).call(this):w(this,b,Fe).call(this)},Fe=function(){I(this,Ee,!0),this.wrapper.classList.add(Panel_module_default.expanded),p(this,Ie).textContent="▲"},Xe=function(){I(this,Ee,!1),this.wrapper.classList.remove(Panel_module_default.expanded),p(this,Ie).textContent="▼"},Et=function(){I(this,Ze,setInterval(()=>{w(this,b,St).call(this)},450))},Tt=function(){p(this,Ze)&&(clearInterval(p(this,Ze)),I(this,Ze,null))},St=function(){if(!p(this,se)||p(this,Re))return;if(p(this,ae).textContent===p(this,se)){I(this,se,null);return}const e=p(this,se);I(this,se,null),w(this,b,zt).call(this,e)},zt=function(e){I(this,Re,!0),p(this,ae).classList.add(Panel_module_default.fadeOut),setTimeout(()=>{p(this,ae).textContent=e,p(this,ae).classList.remove(Panel_module_default.fadeOut),p(this,ae).classList.add(Panel_module_default.fadeIn),setTimeout(()=>{p(this,ae).classList.remove(Panel_module_default.fadeIn),I(this,Re,!1)},300)},150)},Se=function(e){const t=e==="running"?"thinking":e;p(this,Ce).className=Panel_module_default.indicator,t!=="idle"&&t!=="stopped"&&p(this,Ce).classList.add(Panel_module_default[t])},ot=function(){setTimeout(()=>{p(this,ge).scrollTop=p(this,ge).scrollHeight},0)},st=function(){const e=[],t=p(this,H).task;t&&e.push(w(this,b,It).call(this,t));const n=p(this,H).history;for(const r of n)e.push(...w(this,b,Pt).call(this,r));p(this,ge).innerHTML=e.join(""),w(this,b,ot).call(this)},It=function(e){return createCard({icon:"🎯",content:e,type:"input"})},Pt=function(e){const t=[],n=e.type==="step"&&e.stepIndex!==void 0?p(this,J).t("ui.panel.step",{number:(e.stepIndex+1).toString()}):void 0;if(e.type==="step"){if(e.reflection){const o=createReflectionLines(e.reflection);o.length>0&&t.push(createCard({icon:"🧠",content:o,meta:n}))}const r=e.action;r&&t.push(...w(this,b,Zt).call(this,r,n))}else if(e.type==="observation")t.push(createCard({icon:"👁️",content:e.content||"",meta:n,type:"observation"}));else if(e.type==="user_takeover")t.push(createCard({icon:"👤",content:"User takeover",meta:n,type:"input"}));else if(e.type==="retry"){const r=`${e.message||"Retrying"} (${e.attempt}/${e.maxAttempts})`;t.push(createCard({icon:"🔄",content:r,meta:n,type:"observation"}))}else e.type==="error"&&t.push(createCard({icon:"❌",content:e.message||"Error",meta:n,type:"observation"}));return t},Zt=function(e,t){var r;const n=[];if(e.name==="done"){const o=e.input.text||e.output||"";o&&n.push(createCard({icon:"🤖",content:o,meta:t,type:"output"}))}else if(e.name==="ask_user"){const o=e.input,s=e.output.replace(/^User answered:\s*/i,"");n.push(createCard({icon:"❓",content:`Question: ${o.question||""}`,meta:t,type:"question"})),n.push(createCard({icon:"💬",content:`Answer: ${s}`,meta:t,type:"input"}))}else{const o=w(this,b,tt).call(this,e.name,e.input);n.push(createCard({icon:"🔨",content:o,meta:t})),((r=e.output)==null?void 0:r.length)>0&&n.push(createCard({icon:"🔨",content:e.output,meta:t,type:"output"}))}return n},ct),PageAgent=class extends PageAgentCore{constructor(t){const n=new PageController({...t,enableMask:t.enableMask??!0});super({...t,pageController:n});R(this,"panel");this.panel=new Panel(this,{language:t.language,promptForNextTask:t.promptForNextTask})}};export{PageAgent,PageAgentCore,tool};
