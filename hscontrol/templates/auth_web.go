package templates

import (
	"github.com/chasefleming/elem-go"
)

// AuthWeb renders a page that instructs an administrator to run a CLI command
// to complete an authentication or registration flow. consoleURL, when not
// empty, links to the admin console page that does the same under consoleText,
// so an operator with a browser open need not reach for a terminal.
func AuthWeb(title, description, command, consoleURL, consoleText string) *elem.Element {
	content := []elem.Node{
		H1(elem.Text(title)),
	}

	if consoleURL != "" {
		content = append(content, P(A(consoleURL, elem.Text(consoleText)), elem.Text(", or:")))
	}

	content = append(content, P(elem.Text(description)), codeBlockText(command))

	return page(title+" - Headscale", content...)
}
