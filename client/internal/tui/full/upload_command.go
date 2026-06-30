package full

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func (m *model) handleUploadCommand(args []string) error {
	kind, path, id, version, name, platform, publish, err := parseUploadArgs(args)
	if err != nil {
		return err
	}
	var out *nodeapi.ManagePackageUploadResult
	switch kind {
	case "skill", "skills":
		out, err = m.client.UploadSkillToManage(m.ctx, path, id, version, name, publish)
	case "externaltool", "externaltools", "tool", "tools":
		out, err = m.client.UploadExternalToolToManage(m.ctx, path, id, version, name, platform, publish)
	case "plugin", "plugins":
		out, err = m.client.UploadPluginToManage(m.ctx, path, id, version, name, platform, publish)
	default:
		return fmt.Errorf("未知 upload 类型 %q（可用 skill/externaltool/plugin）", kind)
	}
	if err != nil {
		return err
	}
	status := out.Status
	if out.Message != "" {
		status += " — " + out.Message
	}
	body := []string{
		tuishared.PanelNote(fmt.Sprintf("已上传 %s %s@%s（%s）", out.Kind, out.ID, out.Version, status)),
	}
	m.transcript.AddSystemPanel("Manage Upload", body)
	m.syncViewport()
	return nil
}

func parseUploadArgs(args []string) (kind, path, id, version, name, platform string, publish bool, err error) {
	if len(args) < 4 {
		return "", "", "", "", "", "", false, fmt.Errorf(
			"用法: /upload <skill|externaltool|plugin> PATH ID VERSION [NAME] [--platform X] [--publish]",
		)
	}
	kind = strings.ToLower(args[0])
	path = args[1]
	id = args[2]
	version = args[3]
	name = id
	for i := 4; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--publish":
			publish = true
		case "--platform":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", false, fmt.Errorf("--platform 需要参数")
			}
			platform = args[i+1]
			i++
		default:
			if name == id {
				name = a
			} else {
				name += " " + a
			}
		}
	}
	return kind, path, id, version, name, platform, publish, nil
}
