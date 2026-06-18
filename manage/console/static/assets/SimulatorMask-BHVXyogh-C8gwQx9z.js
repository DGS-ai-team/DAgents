var ct=Object.defineProperty;var tt=r=>{throw TypeError(r)};var dt=(r,t,e)=>t in r?ct(r,t,{enumerable:!0,configurable:!0,writable:!0,value:e}):r[t]=e;var l=(r,t,e)=>dt(r,typeof t!="symbol"?t+"":t,e),H=(r,t,e)=>t.has(r)||tt("Cannot "+e);var s=(r,t,e)=>(H(r,t,"read from private field"),e?e.call(r):t.get(r)),k=(r,t,e)=>t.has(r)?tt("Cannot add the same private member more than once"):t instanceof WeakSet?t.add(r):t.set(r,e),u=(r,t,e,o)=>(H(r,t,"write to private field"),o?o.call(r,e):t.set(r,e),e),N=(r,t,e)=>(H(r,t,"access private method"),e);/**
 * AI Motion - WebGL2 animated border with AI-style glow effects
 *
 * @author Simon<gaomeng1900@gmail.com>
 * @license MIT
 * @repository https://github.com/gaomeng1900/ai-motion
 */function et(r,t,e,o){const n=Math.max(1,Math.min(r,t)),i=Math.min(e,20),P=Math.min(i+o,n),m=Math.min(P,Math.floor(r/2)),w=Math.min(P,Math.floor(t/2)),c=j=>j/r*2-1,p=j=>j/t*2-1,G=0,D=r,U=0,T=t,I=m,S=r-m,M=w,O=t-w,h=c(G),d=c(D),W=p(U),V=p(T),z=c(I),Q=c(S),v=p(M),b=p(O),_=0,Y=0,L=1,X=1,$=m/r,q=1-m/r,y=w/t,E=1-w/t,st=new Float32Array([h,W,d,W,h,v,h,v,d,W,d,v,h,b,d,b,h,V,h,V,d,b,d,V,h,v,z,v,h,b,h,b,z,v,z,b,Q,v,d,v,Q,b,Q,b,d,v,d,b]),at=new Float32Array([_,Y,L,Y,_,y,_,y,L,Y,L,y,_,E,L,E,_,X,_,X,L,E,L,X,_,y,$,y,_,E,_,E,$,y,$,E,q,y,L,y,q,E,q,E,L,y,L,E]);return{positions:st,uvs:at}}/**
 * AI Motion - WebGL2 animated border with AI-style glow effects
 *
 * @author Simon<gaomeng1900@gmail.com>
 * @license MIT
 * @repository https://github.com/gaomeng1900/ai-motion
 */function rt(r,t,e){const o=r.createShader(t);if(!o)throw new Error("Failed to create shader");if(r.shaderSource(o,e),r.compileShader(o),!r.getShaderParameter(o,r.COMPILE_STATUS)){const n=r.getShaderInfoLog(o)||"Unknown shader error";throw r.deleteShader(o),new Error(n)}return o}function lt(r,t,e){const o=rt(r,r.VERTEX_SHADER,t),n=rt(r,r.FRAGMENT_SHADER,e),i=r.createProgram();if(!i)throw new Error("Failed to create program");if(r.attachShader(i,o),r.attachShader(i,n),r.linkProgram(i),!r.getProgramParameter(i,r.LINK_STATUS)){const x=r.getProgramInfoLog(i)||"Unknown link error";throw r.deleteProgram(i),r.deleteShader(o),r.deleteShader(n),new Error(x)}return r.deleteShader(o),r.deleteShader(n),i}const ht=`#version 300 es
precision lowp float;
in vec2 vUV;
out vec4 outColor;
uniform vec2 uResolution;
uniform float uTime;
uniform float uBorderWidth;
uniform float uGlowWidth;
uniform float uBorderRadius;
uniform vec3 uColors[4];
uniform float uGlowExponent;
uniform float uGlowFactor;
const float PI = 3.14159265359;
const float TWO_PI = 2.0 * PI;
const float HALF_PI = 0.5 * PI;
const vec4 startPositions = vec4(0.0, PI, HALF_PI, 1.5 * PI);
const vec4 speeds = vec4(-1.9, -1.9, -1.5, 2.1);
const vec4 innerRadius = vec4(PI * 0.8, PI * 0.7, PI * 0.3, PI * 0.1);
const vec4 outerRadius = vec4(PI * 1.2, PI * 0.9, PI * 0.6, PI * 0.4);
float random(vec2 st) {
return fract(sin(dot(st.xy, vec2(12.9898, 78.233))) * 43758.5453123);
}
vec2 random2(vec2 st) {
return vec2(random(st), random(st + 1.0));
}
float aaStep(float edge, float d) {
float width = fwidth(d);
return smoothstep(edge - width * 0.5, edge + width * 0.5, d);
}
float aaFract(float x) {
float f = fract(x);
float w = fwidth(x);
float smooth_f = f * (1.0 - smoothstep(1.0 - w, 1.0, f));
return smooth_f;
}
float sdRoundedBox(in vec2 p, in vec2 b, in float r) {
vec2 q = abs(p) - b + r;
return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r;
}
float getInnerGlow(vec2 p, vec2 b, float radius) {
float dist_x = b.x - abs(p.x);
float dist_y = b.y - abs(p.y);
float glow_x = smoothstep(radius, 0.0, dist_x);
float glow_y = smoothstep(radius, 0.0, dist_y);
return 1.0 - (1.0 - glow_x) * (1.0 - glow_y);
}
float getVignette(vec2 uv) {
vec2 vignetteUv = uv;
vignetteUv = vignetteUv * (1.0 - vignetteUv);
float vignette = vignetteUv.x * vignetteUv.y * 25.0;
vignette = pow(vignette, 0.16);
vignette = 1.0 - vignette;
return vignette;
}
float uvToAngle(vec2 uv) {
vec2 center = vec2(0.5);
vec2 dir = uv - center;
return atan(dir.y, dir.x) + PI;
}
void main() {
vec2 uv = vUV;
vec2 pos = uv * uResolution;
vec2 centeredPos = pos - uResolution * 0.5;
vec2 size = uResolution - uBorderWidth;
vec2 halfSize = size * 0.5;
float dBorderBox = sdRoundedBox(centeredPos, halfSize, uBorderRadius);
float border = aaStep(0.0, dBorderBox);
float glow = getInnerGlow(centeredPos, halfSize, uGlowWidth);
float vignette = getVignette(uv);
glow *= vignette;
float posAngle = uvToAngle(uv);
vec4 lightCenter = mod(startPositions + speeds * uTime, TWO_PI);
vec4 angleDist = abs(posAngle - lightCenter);
vec4 disToLight = min(angleDist, TWO_PI - angleDist) / TWO_PI;
float intensityBorder[4];
intensityBorder[0] = 1.0;
intensityBorder[1] = smoothstep(0.4, 0.0, disToLight.y);
intensityBorder[2] = smoothstep(0.4, 0.0, disToLight.z);
intensityBorder[3] = smoothstep(0.2, 0.0, disToLight.w) * 0.5;
vec3 borderColor = vec3(0.0);
for(int i = 0; i < 4; i++) {
borderColor = mix(borderColor, uColors[i], intensityBorder[i]);
}
borderColor *= 1.1;
borderColor = clamp(borderColor, 0.0, 1.0);
float intensityGlow[4];
intensityGlow[0] = smoothstep(0.9, 0.0, disToLight.x);
intensityGlow[1] = smoothstep(0.7, 0.0, disToLight.y);
intensityGlow[2] = smoothstep(0.4, 0.0, disToLight.z);
intensityGlow[3] = smoothstep(0.1, 0.0, disToLight.w) * 0.7;
vec4 breath = smoothstep(0.0, 1.0, sin(uTime * 1.0 + startPositions * PI) * 0.2 + 0.8);
vec3 glowColor = vec3(0.0);
glowColor += uColors[0] * intensityGlow[0] * breath.x;
glowColor += uColors[1] * intensityGlow[1] * breath.y;
glowColor += uColors[2] * intensityGlow[2] * breath.z;
glowColor += uColors[3] * intensityGlow[3] * breath.w * glow;
glow = pow(glow, uGlowExponent);
glow *= random(pos + uTime) * 0.1 + 1.0;
glowColor *= glow * uGlowFactor;
glowColor = clamp(glowColor, 0.0, 1.0);
vec3 color = mix(glowColor, borderColor + glowColor * 0.2, border);
float alpha = mix(glow, 1.0, border);
outColor = vec4(color, alpha);
}`,ut=`#version 300 es
in vec2 aPosition;
in vec2 aUV;
out vec2 vUV;
void main() {
vUV = aUV;
gl_Position = vec4(aPosition, 0.0, 1.0);
}`;/**
 * AI Motion - WebGL2 animated border with AI-style glow effects
 *
 * @author Simon<gaomeng1900@gmail.com>
 * @license MIT
 * @repository https://github.com/gaomeng1900/ai-motion
 */const ft=["rgb(57, 182, 255)","rgb(189, 69, 251)","rgb(255, 87, 51)","rgb(255, 214, 0)"];function gt(r){const t=r.match(/rgb\((\d+),\s*(\d+),\s*(\d+)\)/);if(!t)throw new Error(`Invalid color format: ${r}`);const[,e,o,n]=t;return[parseInt(e)/255,parseInt(o)/255,parseInt(n)/255]}class pt{constructor(t={}){l(this,"element");l(this,"canvas");l(this,"options");l(this,"running",!1);l(this,"disposed",!1);l(this,"startTime",0);l(this,"lastTime",0);l(this,"rafId",null);l(this,"glr");l(this,"observer");this.options={width:t.width??600,height:t.height??600,ratio:t.ratio??window.devicePixelRatio??1,borderWidth:t.borderWidth??8,glowWidth:t.glowWidth??200,borderRadius:t.borderRadius??8,mode:t.mode??"light",...t},this.canvas=document.createElement("canvas"),this.options.classNames&&(this.canvas.className=this.options.classNames),this.options.styles&&Object.assign(this.canvas.style,this.options.styles),this.canvas.style.display="block",this.canvas.style.transformOrigin="center",this.canvas.style.pointerEvents="none",this.element=this.canvas,this.setupGL(),this.options.skipGreeting||this.greet()}start(){if(this.disposed)throw new Error("Motion instance has been disposed.");if(this.running)return;if(!this.glr){console.error("WebGL resources are not initialized.");return}this.running=!0,this.startTime=performance.now(),this.resize(this.options.width??600,this.options.height??600,this.options.ratio),this.glr.gl.viewport(0,0,this.canvas.width,this.canvas.height),this.glr.gl.useProgram(this.glr.program),this.glr.gl.uniform2f(this.glr.uResolution,this.canvas.width,this.canvas.height),this.checkGLError(this.glr.gl,"start: after initial setup");const t=()=>{if(!this.running||!this.glr)return;this.rafId=requestAnimationFrame(t);const e=performance.now();if(e-this.lastTime<1e3/32)return;this.lastTime=e;const n=(e-this.startTime)*.001;this.render(n)};this.rafId=requestAnimationFrame(t)}pause(){if(this.disposed)throw new Error("Motion instance has been disposed.");this.running=!1,this.rafId!==null&&cancelAnimationFrame(this.rafId)}dispose(){if(this.disposed)return;this.disposed=!0,this.running=!1,this.rafId!==null&&cancelAnimationFrame(this.rafId);const{gl:t,vao:e,positionBuffer:o,uvBuffer:n,program:i}=this.glr;e&&t.deleteVertexArray(e),o&&t.deleteBuffer(o),n&&t.deleteBuffer(n),t.deleteProgram(i),this.observer&&this.observer.disconnect(),this.canvas.remove()}resize(t,e,o){if(this.disposed)throw new Error("Motion instance has been disposed.");if(this.options.width=t,this.options.height=e,o&&(this.options.ratio=o),!this.running)return;const{gl:n,program:i,vao:x,positionBuffer:P,uvBuffer:m,uResolution:w}=this.glr,c=o??this.options.ratio??window.devicePixelRatio??1,p=Math.max(1,Math.floor(t*c)),G=Math.max(1,Math.floor(e*c));this.canvas.style.width=`${t}px`,this.canvas.style.height=`${e}px`,(this.canvas.width!==p||this.canvas.height!==G)&&(this.canvas.width=p,this.canvas.height=G),n.viewport(0,0,this.canvas.width,this.canvas.height),this.checkGLError(n,"resize: after viewport setup");const{positions:D,uvs:U}=et(this.canvas.width,this.canvas.height,this.options.borderWidth*c,this.options.glowWidth*c);n.bindVertexArray(x),n.bindBuffer(n.ARRAY_BUFFER,P),n.bufferData(n.ARRAY_BUFFER,D,n.STATIC_DRAW);const T=n.getAttribLocation(i,"aPosition");n.enableVertexAttribArray(T),n.vertexAttribPointer(T,2,n.FLOAT,!1,0,0),this.checkGLError(n,"resize: after position buffer update"),n.bindBuffer(n.ARRAY_BUFFER,m),n.bufferData(n.ARRAY_BUFFER,U,n.STATIC_DRAW);const I=n.getAttribLocation(i,"aUV");n.enableVertexAttribArray(I),n.vertexAttribPointer(I,2,n.FLOAT,!1,0,0),this.checkGLError(n,"resize: after UV buffer update"),n.useProgram(i),n.uniform2f(w,this.canvas.width,this.canvas.height),n.uniform1f(this.glr.uBorderWidth,this.options.borderWidth*c),n.uniform1f(this.glr.uGlowWidth,this.options.glowWidth*c),n.uniform1f(this.glr.uBorderRadius,this.options.borderRadius*c),this.checkGLError(n,"resize: after uniform updates");const S=performance.now();this.lastTime=S;const M=(S-this.startTime)*.001;this.render(M)}autoResize(t){this.observer&&this.observer.disconnect(),this.observer=new ResizeObserver(()=>{const e=t.getBoundingClientRect();this.resize(e.width,e.height)}),this.observer.observe(t)}fadeIn(){if(this.disposed)throw new Error("Motion instance has been disposed.");return new Promise((t,e)=>{const o=this.canvas.animate([{opacity:0,transform:"scale(1.2)"},{opacity:1,transform:"scale(1)"}],{duration:300,easing:"ease-out",fill:"forwards"});o.onfinish=()=>t(),o.oncancel=()=>e("canceled")})}fadeOut(){if(this.disposed)throw new Error("Motion instance has been disposed.");return new Promise((t,e)=>{const o=this.canvas.animate([{opacity:1,transform:"scale(1)"},{opacity:0,transform:"scale(1.2)"}],{duration:300,easing:"ease-in",fill:"forwards"});o.onfinish=()=>t(),o.oncancel=()=>e("canceled")})}checkGLError(t,e){let o=t.getError();if(o!==t.NO_ERROR){for(console.group(`🔴 WebGL Error in ${e}`);o!==t.NO_ERROR;){const n=this.getGLErrorName(t,o);console.error(`${n} (0x${o.toString(16)})`),o=t.getError()}console.groupEnd()}}getGLErrorName(t,e){switch(e){case t.INVALID_ENUM:return"INVALID_ENUM";case t.INVALID_VALUE:return"INVALID_VALUE";case t.INVALID_OPERATION:return"INVALID_OPERATION";case t.INVALID_FRAMEBUFFER_OPERATION:return"INVALID_FRAMEBUFFER_OPERATION";case t.OUT_OF_MEMORY:return"OUT_OF_MEMORY";case t.CONTEXT_LOST_WEBGL:return"CONTEXT_LOST_WEBGL";default:return"UNKNOWN_ERROR"}}setupGL(){const t=this.canvas.getContext("webgl2",{antialias:!1,alpha:!0});if(!t)throw new Error("WebGL2 is required but not available.");const e=lt(t,ut,ht);this.checkGLError(t,"setupGL: after createProgram");const o=t.createVertexArray();t.bindVertexArray(o),this.checkGLError(t,"setupGL: after VAO creation");const n=this.canvas.width||2,i=this.canvas.height||2,{positions:x,uvs:P}=et(n,i,this.options.borderWidth,this.options.glowWidth),m=t.createBuffer();t.bindBuffer(t.ARRAY_BUFFER,m),t.bufferData(t.ARRAY_BUFFER,x,t.STATIC_DRAW);const w=t.getAttribLocation(e,"aPosition");t.enableVertexAttribArray(w),t.vertexAttribPointer(w,2,t.FLOAT,!1,0,0),this.checkGLError(t,"setupGL: after position buffer setup");const c=t.createBuffer();t.bindBuffer(t.ARRAY_BUFFER,c),t.bufferData(t.ARRAY_BUFFER,P,t.STATIC_DRAW);const p=t.getAttribLocation(e,"aUV");t.enableVertexAttribArray(p),t.vertexAttribPointer(p,2,t.FLOAT,!1,0,0),this.checkGLError(t,"setupGL: after UV buffer setup");const G=t.getUniformLocation(e,"uResolution"),D=t.getUniformLocation(e,"uTime"),U=t.getUniformLocation(e,"uBorderWidth"),T=t.getUniformLocation(e,"uGlowWidth"),I=t.getUniformLocation(e,"uBorderRadius"),S=t.getUniformLocation(e,"uColors"),M=t.getUniformLocation(e,"uGlowExponent"),O=t.getUniformLocation(e,"uGlowFactor");t.useProgram(e),t.uniform1f(U,this.options.borderWidth),t.uniform1f(T,this.options.glowWidth),t.uniform1f(I,this.options.borderRadius),this.options.mode==="dark"?(t.uniform1f(M,2),t.uniform1f(O,1.8)):(t.uniform1f(M,1),t.uniform1f(O,1));const h=(this.options.colors||ft).map(gt);for(let d=0;d<h.length;d++)t.uniform3f(t.getUniformLocation(e,`uColors[${d}]`),...h[d]);this.checkGLError(t,"setupGL: after uniform setup"),t.bindVertexArray(null),t.bindBuffer(t.ARRAY_BUFFER,null),this.glr={gl:t,program:e,vao:o,positionBuffer:m,uvBuffer:c,uResolution:G,uTime:D,uBorderWidth:U,uGlowWidth:T,uBorderRadius:I,uColors:S}}render(t){if(!this.glr)return;const{gl:e,program:o,vao:n,uTime:i}=this.glr;e.useProgram(o),e.bindVertexArray(n),e.uniform1f(i,t),e.disable(e.DEPTH_TEST),e.disable(e.CULL_FACE),e.disable(e.BLEND),e.clearColor(0,0,0,0),e.clear(e.COLOR_BUFFER_BIT),e.drawArrays(e.TRIANGLES,0,24),this.checkGLError(e,"render: after draw call"),e.bindVertexArray(null)}greet(){console.log("%c🌈 ai-motion 0.4.8 🌈","background: linear-gradient(90deg, #39b6ff, #bd45fb, #ff5733, #ffd600); color: white; text-shadow: 0 0 2px rgba(0, 0, 0, 0.2); font-weight: bold; font-size: 1em; padding: 2px 12px; border-radius: 6px;")}}(function(){try{if(typeof document<"u"){var r=document.createElement("style");r.appendChild(document.createTextNode(`._wrapper_1ooyb_1 {
	position: fixed;
	inset: 0;
	z-index: 2147483641; /* 确保在所有元素之上，除了 panel */
	cursor: wait;
	overflow: hidden;

	display: none;
}

._wrapper_1ooyb_1._visible_1ooyb_11 {
	display: block;
}
/* AI 光标样式 */
._cursor_1dgwb_2 {
	position: absolute;
	width: var(--cursor-size, 75px);
	height: var(--cursor-size, 75px);
	pointer-events: none;
	z-index: 10000;
}

._cursorBorder_1dgwb_10 {
	position: absolute;
	width: 100%;
	height: 100%;
	background: linear-gradient(45deg, rgb(57, 182, 255), rgb(189, 69, 251));
	mask-image: url("data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20100%20100'%20fill='none'%3e%3cg%3e%3cpath%20d='M%2015%2042%20L%2015%2036.99%20Q%2015%2031.99%2023.7%2031.99%20L%2028.05%2031.99%20Q%2032.41%2031.99%2032.41%2021.99%20L%2032.41%2017%20Q%2032.41%2012%2041.09%2016.95%20L%2076.31%2037.05%20Q%2085%2042%2076.31%2046.95%20L%2041.09%2067.05%20Q%2032.41%2072%2032.41%2062.01%20L%2032.41%2057.01%20Q%2032.41%2052.01%2023.7%2052.01%20L%2019.35%2052.01%20Q%2015%2052.01%2015%2047.01%20Z'%20fill='none'%20stroke='%23000000'%20stroke-width='6'%20stroke-miterlimit='10'%20style='stroke:%20light-dark(rgb(0,%200,%200),%20rgb(255,%20255,%20255));'/%3e%3c/g%3e%3c/svg%3e");
	mask-size: 100% 100%;
	mask-repeat: no-repeat;

	transform-origin: center;
	transform: rotate(-135deg) scale(1.2);
	margin-left: -10px;
	margin-top: -18px;
}

._cursorFilling_1dgwb_25 {
	position: absolute;
	width: 100%;
	height: 100%;
	background: url("data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20100%20100'%3e%3cdefs%3e%3c/defs%3e%3cg%20xmlns='http://www.w3.org/2000/svg'%20style='filter:%20drop-shadow(light-dark(rgba(0,%200,%200,%200.4),%20rgba(237,%20237,%20237,%200.4))%203px%204px%204px);'%3e%3cpath%20d='M%2015%2042%20L%2015%2036.99%20Q%2015%2031.99%2023.7%2031.99%20L%2028.05%2031.99%20Q%2032.41%2031.99%2032.41%2021.99%20L%2032.41%2017%20Q%2032.41%2012%2041.09%2016.95%20L%2076.31%2037.05%20Q%2085%2042%2076.31%2046.95%20L%2041.09%2067.05%20Q%2032.41%2072%2032.41%2062.01%20L%2032.41%2057.01%20Q%2032.41%2052.01%2023.7%2052.01%20L%2019.35%2052.01%20Q%2015%2052.01%2015%2047.01%20Z'%20fill='%23ffffff'%20stroke='none'%20style='fill:%20%23ffffff;'/%3e%3c/g%3e%3c/svg%3e");
	background-size: 100% 100%;
	background-repeat: no-repeat;

	transform-origin: center;
	transform: rotate(-135deg) scale(1.2);
	margin-left: -10px;
	margin-top: -18px;
}

._cursorRipple_1dgwb_39 {
	position: absolute;
	width: 100%;
	height: 100%;
	pointer-events: none;
	margin-left: -50%;
	margin-top: -50%;

	&::after {
		content: '';
		opacity: 0;
		position: absolute;
		inset: 0;
		border: 4px solid rgba(57, 182, 255, 1);
		border-radius: 50%;
	}
}

._cursor_1dgwb_2._clicking_1dgwb_57 ._cursorRipple_1dgwb_39::after {
	animation: _cursor-ripple_1dgwb_1 300ms ease-out forwards;
}

@keyframes _cursor-ripple_1dgwb_1 {
	0% {
		transform: scale(0);
		opacity: 1;
	}
	100% {
		transform: scale(2);
		opacity: 0;
	}
}`)),document.head.appendChild(r)}}catch(t){console.error("vite-plugin-css-injected-by-js",t)}})();(function(){try{if(typeof document<"u"){var r=document.createElement("style");r.appendChild(document.createTextNode(`._wrapper_1ooyb_1 {
	position: fixed;
	inset: 0;
	z-index: 2147483641; /* 确保在所有元素之上，除了 panel */
	cursor: wait;
	overflow: hidden;

	display: none;
}

._wrapper_1ooyb_1._visible_1ooyb_11 {
	display: block;
}
/* AI 光标样式 */
._cursor_1dgwb_2 {
	position: absolute;
	width: var(--cursor-size, 75px);
	height: var(--cursor-size, 75px);
	pointer-events: none;
	z-index: 10000;
}

._cursorBorder_1dgwb_10 {
	position: absolute;
	width: 100%;
	height: 100%;
	background: linear-gradient(45deg, rgb(57, 182, 255), rgb(189, 69, 251));
	mask-image: url("data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20100%20100'%20fill='none'%3e%3cg%3e%3cpath%20d='M%2015%2042%20L%2015%2036.99%20Q%2015%2031.99%2023.7%2031.99%20L%2028.05%2031.99%20Q%2032.41%2031.99%2032.41%2021.99%20L%2032.41%2017%20Q%2032.41%2012%2041.09%2016.95%20L%2076.31%2037.05%20Q%2085%2042%2076.31%2046.95%20L%2041.09%2067.05%20Q%2032.41%2072%2032.41%2062.01%20L%2032.41%2057.01%20Q%2032.41%2052.01%2023.7%2052.01%20L%2019.35%2052.01%20Q%2015%2052.01%2015%2047.01%20Z'%20fill='none'%20stroke='%23000000'%20stroke-width='6'%20stroke-miterlimit='10'%20style='stroke:%20light-dark(rgb(0,%200,%200),%20rgb(255,%20255,%20255));'/%3e%3c/g%3e%3c/svg%3e");
	mask-size: 100% 100%;
	mask-repeat: no-repeat;

	transform-origin: center;
	transform: rotate(-135deg) scale(1.2);
	margin-left: -10px;
	margin-top: -18px;
}

._cursorFilling_1dgwb_25 {
	position: absolute;
	width: 100%;
	height: 100%;
	background: url("data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%20100%20100'%3e%3cdefs%3e%3c/defs%3e%3cg%20xmlns='http://www.w3.org/2000/svg'%20style='filter:%20drop-shadow(light-dark(rgba(0,%200,%200,%200.4),%20rgba(237,%20237,%20237,%200.4))%203px%204px%204px);'%3e%3cpath%20d='M%2015%2042%20L%2015%2036.99%20Q%2015%2031.99%2023.7%2031.99%20L%2028.05%2031.99%20Q%2032.41%2031.99%2032.41%2021.99%20L%2032.41%2017%20Q%2032.41%2012%2041.09%2016.95%20L%2076.31%2037.05%20Q%2085%2042%2076.31%2046.95%20L%2041.09%2067.05%20Q%2032.41%2072%2032.41%2062.01%20L%2032.41%2057.01%20Q%2032.41%2052.01%2023.7%2052.01%20L%2019.35%2052.01%20Q%2015%2052.01%2015%2047.01%20Z'%20fill='%23ffffff'%20stroke='none'%20style='fill:%20%23ffffff;'/%3e%3c/g%3e%3c/svg%3e");
	background-size: 100% 100%;
	background-repeat: no-repeat;

	transform-origin: center;
	transform: rotate(-135deg) scale(1.2);
	margin-left: -10px;
	margin-top: -18px;
}

._cursorRipple_1dgwb_39 {
	position: absolute;
	width: 100%;
	height: 100%;
	pointer-events: none;
	margin-left: -50%;
	margin-top: -50%;

	&::after {
		content: '';
		opacity: 0;
		position: absolute;
		inset: 0;
		border: 4px solid rgba(57, 182, 255, 1);
		border-radius: 50%;
	}
}

._cursor_1dgwb_2._clicking_1dgwb_57 ._cursorRipple_1dgwb_39::after {
	animation: _cursor-ripple_1dgwb_1 300ms ease-out forwards;
}

@keyframes _cursor-ripple_1dgwb_1 {
	0% {
		transform: scale(0);
		opacity: 1;
	}
	100% {
		transform: scale(2);
		opacity: 0;
	}
}`)),document.head.appendChild(r)}}catch(t){console.error("vite-plugin-css-injected-by-js",t)}})();function mt(){try{return!!(wt()||vt()||bt()||_t()||yt()||Lt())}catch(r){return console.warn("Error determining if page is dark:",r),!1}}function wt(){const r=["dark","dark-mode","theme-dark","night","night-mode"],t=document.documentElement,e=document.body||document.documentElement;for(const o of r)if(t.classList.contains(o)||e!=null&&e.classList.contains(o))return!0;return!1}function vt(){const r=document.documentElement,t=document.body||document.documentElement;for(const e of["data-theme","data-color-mode","data-bs-theme","data-mui-color-scheme"]){const o=t==null?void 0:t.getAttribute(e),n=r.getAttribute(e);if((o==null?void 0:o.toLowerCase())==="dark"||(n==null?void 0:n.toLowerCase())==="dark")return!0}return!1}function bt(){var e;const r=(e=document.querySelector('meta[name="color-scheme"]'))==null?void 0:e.content.toLowerCase();if(r==="dark"||r==="only dark")return!0;const t=window.getComputedStyle(document.documentElement).getPropertyValue("color-scheme").trim().toLowerCase();return t==="dark"||t==="only dark"}function _t(){const r=window.getComputedStyle(document.documentElement),t=window.getComputedStyle(document.body||document.documentElement),e=r.backgroundColor,o=t.backgroundColor;return K(o)?!0:o==="transparent"||o.startsWith("rgba(0, 0, 0, 0)")?K(e):!1}function Lt(){const t=nt(window.getComputedStyle(document.body||document.documentElement).color);return t!==null&&t>200}function yt(){const{innerWidth:r,innerHeight:t}=window,e=r*t*.5;for(const o of["#app","#root","#__next"]){const n=document.querySelector(o);if(!n)continue;const i=n.getBoundingClientRect();if(!(i.width*i.height<e)&&K(window.getComputedStyle(n).backgroundColor))return!0}return!1}function Et(r){const t=/rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(r);return t?{r:parseInt(t[1]),g:parseInt(t[2]),b:parseInt(t[3])}:null}function nt(r){if(!r||r==="transparent"||r.startsWith("rgba(0, 0, 0, 0)"))return null;const t=Et(r);return t?.299*t.r+.587*t.g+.114*t.b:null}function K(r,t=128){const e=nt(r);return e!==null&&e<t}var Z={wrapper:"_wrapper_1ooyb_1",visible:"_visible_1ooyb_11"},B={cursor:"_cursor_1dgwb_2",cursorBorder:"_cursorBorder_1dgwb_10",cursorFilling:"_cursorFilling_1dgwb_25",cursorRipple:"_cursorRipple_1dgwb_39",clicking:"_clicking_1dgwb_57"},A,a,f,g,C,R,F,it,J,ot,xt=(ot=class extends EventTarget{constructor(){super();k(this,F);l(this,"shown",!1);l(this,"wrapper",document.createElement("div"));l(this,"motion",null);k(this,A,!1);k(this,a,document.createElement("div"));k(this,f,0);k(this,g,0);k(this,C,0);k(this,R,0);this.wrapper.id="page-agent-runtime_simulator-mask",this.wrapper.className=Z.wrapper,this.wrapper.setAttribute("data-browser-use-ignore","true"),this.wrapper.setAttribute("data-page-agent-ignore","true");try{const i=new pt({mode:mt()?"dark":"light",styles:{position:"absolute",inset:"0"}});this.motion=i,this.wrapper.appendChild(i.element),i.autoResize(this.wrapper)}catch(i){console.warn("[SimulatorMask] Motion overlay unavailable:",i)}this.wrapper.addEventListener("click",i=>{i.stopPropagation(),i.preventDefault()}),this.wrapper.addEventListener("mousedown",i=>{i.stopPropagation(),i.preventDefault()}),this.wrapper.addEventListener("mouseup",i=>{i.stopPropagation(),i.preventDefault()}),this.wrapper.addEventListener("mousemove",i=>{i.stopPropagation(),i.preventDefault()}),this.wrapper.addEventListener("wheel",i=>{i.stopPropagation(),i.preventDefault()}),this.wrapper.addEventListener("keydown",i=>{i.stopPropagation(),i.preventDefault()}),this.wrapper.addEventListener("keyup",i=>{i.stopPropagation(),i.preventDefault()}),N(this,F,it).call(this),document.body.appendChild(this.wrapper),N(this,F,J).call(this);const t=i=>{const{x,y:P}=i.detail;this.setCursorPosition(x,P)},e=()=>{this.triggerClickAnimation()},o=()=>{this.wrapper.style.pointerEvents="none"},n=()=>{this.wrapper.style.pointerEvents="auto"};window.addEventListener("PageAgent::MovePointerTo",t),window.addEventListener("PageAgent::ClickPointer",e),window.addEventListener("PageAgent::EnablePassThrough",o),window.addEventListener("PageAgent::DisablePassThrough",n),this.addEventListener("dispose",()=>{window.removeEventListener("PageAgent::MovePointerTo",t),window.removeEventListener("PageAgent::ClickPointer",e),window.removeEventListener("PageAgent::EnablePassThrough",o),window.removeEventListener("PageAgent::DisablePassThrough",n)})}setCursorPosition(t,e){s(this,A)||(u(this,C,t),u(this,R,e))}triggerClickAnimation(){s(this,A)||(s(this,a).classList.remove(B.clicking),s(this,a).offsetHeight,s(this,a).classList.add(B.clicking))}show(){var t,e;this.shown||s(this,A)||(this.shown=!0,(t=this.motion)==null||t.start(),(e=this.motion)==null||e.fadeIn(),this.wrapper.classList.add(Z.visible),u(this,f,window.innerWidth/2),u(this,g,window.innerHeight/2),u(this,C,s(this,f)),u(this,R,s(this,g)),s(this,a).style.left=`${s(this,f)}px`,s(this,a).style.top=`${s(this,g)}px`)}hide(){var t,e;!this.shown||s(this,A)||(this.shown=!1,(t=this.motion)==null||t.fadeOut(),(e=this.motion)==null||e.pause(),s(this,a).classList.remove(B.clicking),setTimeout(()=>{this.wrapper.classList.remove(Z.visible)},800))}dispose(){var t;u(this,A,!0),(t=this.motion)==null||t.dispose(),this.wrapper.remove(),this.dispatchEvent(new Event("dispose"))}},A=new WeakMap,a=new WeakMap,f=new WeakMap,g=new WeakMap,C=new WeakMap,R=new WeakMap,F=new WeakSet,it=function(){s(this,a).className=B.cursor;const t=document.createElement("div");t.className=B.cursorRipple,s(this,a).appendChild(t);const e=document.createElement("div");e.className=B.cursorFilling,s(this,a).appendChild(e);const o=document.createElement("div");o.className=B.cursorBorder,s(this,a).appendChild(o),this.wrapper.appendChild(s(this,a))},J=function(){if(s(this,A))return;const t=s(this,f)+(s(this,C)-s(this,f))*.2,e=s(this,g)+(s(this,R)-s(this,g))*.2,o=Math.abs(t-s(this,C));o>0&&(o<2?u(this,f,s(this,C)):u(this,f,t),s(this,a).style.left=`${s(this,f)}px`);const n=Math.abs(e-s(this,R));n>0&&(n<2?u(this,g,s(this,R)):u(this,g,e),s(this,a).style.top=`${s(this,g)}px`),requestAnimationFrame(()=>N(this,F,J).call(this))},ot);export{xt as SimulatorMask};
