package tools

import "testing"

func TestDecodePowerShellCLIXMLErrors(t *testing.T) {
	input := `#< CLIXML
<Objs Version="1.1.0.1" xmlns="http://schemas.microsoft.com/powershell/2004/04"><Obj S="progress"><MS><PR><T>Completed</T></PR></MS></Obj><S S="Error">Write-Error : demo-error_x000D__x000A_</S><S S="Error">+ CategoryInfo : NotSpecified_x000D__x000A_</S><S S="Error">+ FullyQualifiedErrorId : DemoError_x000D__x000A_</S></Objs>`

	got := decodePowerShellCLIXML(input)
	want := "Write-Error : demo-error\n+ CategoryInfo : NotSpecified\n+ FullyQualifiedErrorId : DemoError"
	if got != want {
		t.Fatalf("decoded CLIXML = %q, want %q", got, want)
	}
}

func TestDecodePowerShellCLIXMLKeepsUsefulNonErrorStreams(t *testing.T) {
	input := `#< CLIXML
<Objs Version="1.1.0.1"><S S="Warning">warning text_x000D__x000A_</S><S S="progress">ignored progress</S><S S="Verbose">verbose text</S></Objs>`

	got := decodePowerShellCLIXML(input)
	want := "Warning: warning text\nVerbose: verbose text"
	if got != want {
		t.Fatalf("decoded streams = %q, want %q", got, want)
	}
}

func TestDecodePowerShellCLIXMLFallback(t *testing.T) {
	for _, input := range []string{
		"plain stderr",
		"#< CLIXML\n<Objs>",
		"#< CLIXML",
	} {
		if got := decodePowerShellCLIXML(input); got != input {
			t.Fatalf("input %q changed to %q", input, got)
		}
	}
}
