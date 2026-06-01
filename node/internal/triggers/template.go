package triggers

import (
	"encoding/json"
)

// RenderTaskTemplate 渲染 task_template 占位符。

// 支持：trigger_id、trigger_name、reason、payload_json；未知占位符保留 `{key}`。
func RenderTaskTemplate(template string, def Definition, reason string, payload map[string]any) string {
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, _ := json.Marshal(payload)
	values := map[string]string{
		"trigger_id":   def.TriggerID,
		"trigger_name": def.Name,
		"reason":       reason,
		"payload_json": string(payloadJSON),
	}
	return renderBraceTemplate(template, values)
}

func renderBraceTemplate(template string, values map[string]string) string {
	out := make([]byte, 0, len(template))
	for i := 0; i < len(template); i++ {
		if template[i] != '{' {
			out = append(out, template[i])
			continue
		}
		j := i + 1
		for j < len(template) && template[j] != '}' {
			j++
		}
		if j >= len(template) {
			out = append(out, template[i:]...)
			break
		}
		key := template[i+1 : j]
		if v, ok := values[key]; ok {
			out = append(out, v...)
		} else {
			out = append(out, '{')
			out = append(out, key...)
			out = append(out, '}')
		}
		i = j
	}
	return string(out)
}
