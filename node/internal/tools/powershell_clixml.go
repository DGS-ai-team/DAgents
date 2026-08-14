package tools

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
)

// decodePowerShellCLIXML converts the structured error stream emitted by
// Windows PowerShell when stderr is redirected to a pipe into readable text.
// Windows PowerShell 5.1 prefixes this stream with "#< CLIXML".
func decodePowerShellCLIXML(stderr string) string {
	trimmed := strings.TrimSpace(stderr)
	if !strings.HasPrefix(trimmed, "#< CLIXML") {
		return stderr
	}

	newline := strings.IndexByte(trimmed, '\n')
	if newline < 0 {
		return stderr
	}
	xmlText := strings.TrimSpace(trimmed[newline+1:])
	if xmlText == "" {
		return ""
	}

	decoder := xml.NewDecoder(strings.NewReader(xmlText))
	var messages []string
	parsed := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return stderr
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Objs" {
			continue
		}
		parsed = true
		if err := collectPowerShellCLIXMLStrings(decoder, &messages); err != nil {
			return stderr
		}
		break
	}
	if !parsed {
		return stderr
	}

	return strings.Join(messages, "\n")
}

func collectPowerShellCLIXMLStrings(decoder *xml.Decoder, messages *[]string) error {
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local != "S" {
				continue
			}
			stream := ""
			for _, attr := range value.Attr {
				if attr.Name.Local == "S" {
					stream = attr.Value
					break
				}
			}
			var text string
			if err := decoder.DecodeElement(&text, &value); err != nil {
				return err
			}
			text = normalizePowerShellCLIXMLText(text)
			if text == "" || strings.EqualFold(stream, "progress") {
				continue
			}
			if !strings.EqualFold(stream, "error") {
				text = stream + ": " + text
			}
			if !containsPowerShellCLIXMLMessage(*messages, text) {
				*messages = append(*messages, text)
			}
		case xml.EndElement:
			if value.Name.Local == "Objs" {
				return nil
			}
		}
	}
}

func normalizePowerShellCLIXMLText(text string) string {
	text = decodePowerShellXMLEscapes(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSpace(text)
}

// PowerShell escapes control characters in CLIXML as _xHHHH_. Decode UTF-16
// units as a group so surrogate pairs in error text remain valid UTF-8.
func decodePowerShellXMLEscapes(text string) string {
	var out strings.Builder
	var escaped []uint16
	flush := func() {
		if len(escaped) > 0 {
			out.WriteString(string(utf16.Decode(escaped)))
			escaped = escaped[:0]
		}
	}
	for i := 0; i < len(text); {
		if i+7 <= len(text) && text[i] == '_' && text[i+1] == 'x' && text[i+6] == '_' {
			value, err := strconv.ParseUint(text[i+2:i+6], 16, 16)
			if err == nil {
				escaped = append(escaped, uint16(value))
				i += 7
				continue
			}
		}
		flush()
		for _, r := range text[i:] {
			out.WriteRune(r)
			i += len(string(r))
			break
		}
	}
	flush()
	return out.String()
}

func containsPowerShellCLIXMLMessage(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
