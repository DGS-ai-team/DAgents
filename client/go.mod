module github.com/DGS-ai-team/DAgents/client

go 1.25.0

require (
	github.com/DGS-ai-team/DAgents/shared/config v0.0.0
	github.com/DGS-ai-team/DAgents/shared/update v0.0.0
)

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace github.com/DGS-ai-team/DAgents/shared/config => ../shared/config

replace github.com/DGS-ai-team/DAgents/shared/update => ../shared/update
