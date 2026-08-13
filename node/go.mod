module github.com/DGS-ai-team/DAgents/node

go 1.25.0

require (
	github.com/DGS-ai-team/DAgents/shared/config v0.0.0
	github.com/DGS-ai-team/DAgents/shared/update v0.0.0
	github.com/DGS-ai-team/DAgents/shared/workgroup v0.0.0
	github.com/bmatcuk/doublestar/v4 v4.10.0
	github.com/coder/websocket v1.8.13
	github.com/google/uuid v1.6.0
	golang.org/x/sys v0.47.0
	golang.org/x/text v0.40.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.51.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace github.com/DGS-ai-team/DAgents/shared/config => ../shared/config

replace github.com/DGS-ai-team/DAgents/shared/update => ../shared/update

replace github.com/DGS-ai-team/DAgents/shared/workgroup => ../shared/workgroup
