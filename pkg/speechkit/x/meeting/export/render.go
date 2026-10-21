package export

import (
	"fmt"
	"html"
	"strings"
	"unicode/utf8"
)

// Markdown renders the outline as a Markdown file. Text is neutralised so a
// viewer that renders HTML or images loads nothing: "<" is escaped and image
// syntax "![" loses its "!".
func (o Outline) Markdown() string {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n", markdownText(o.Title))
	for _, block := range o.Blocks {
		out.WriteString("\n")
		switch block.Kind {
		case BlockHeading:
			level := clampLevel(block.Level)
			fmt.Fprintf(&out, "%s %s\n", strings.Repeat("#", level), markdownText(block.Text))
		case BlockParagraph:
			if block.Emphasis {
				fmt.Fprintf(&out, "_%s_\n", markdownText(block.Text))
			} else {
				fmt.Fprintf(&out, "%s\n", markdownText(block.Text))
			}
		case BlockList:
			for index, item := range block.Items {
				marker := "- "
				if block.Ordered {
					marker = fmt.Sprintf("%d. ", index+1)
				}
				indent := strings.Repeat(" ", len(marker))
				lines := strings.Split(markdownText(item), "\n")
				out.WriteString(marker + lines[0])
				for _, line := range lines[1:] {
					// A hard break keeps a multi-line note one item.
					out.WriteString("  \n" + indent + line)
				}
				out.WriteString("\n")
			}
		case BlockImage:
			fmt.Fprintf(&out, "[image: %s]\n", markdownText(block.ImageID))
		}
	}
	return out.String()
}

func markdownText(value string) string {
	value = cleanText(value)
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, "![", `!\[`)
	return value
}

// XHTML renders the outline as a complete XHTML page in the subset Microsoft
// OneNote accepts as page input: html, head, title, meta, body, h1 to h6,
// p, ul, ol, li, i and img. It is well-formed XML in UTF-8; images reference
// multipart parts as src="name:<ImagePartName(id)>".
func (o Outline) XHTML() string {
	var out strings.Builder
	out.WriteString("<!DOCTYPE html>\n")
	if lang := languageAttr(o.Locale); lang != "" {
		fmt.Fprintf(&out, "<html lang=\"%s\">\n", lang)
	} else {
		out.WriteString("<html>\n")
	}
	out.WriteString("<head>\n")
	fmt.Fprintf(&out, "<title>%s</title>\n", xmlText(o.Title))
	out.WriteString("<meta http-equiv=\"Content-Type\" content=\"text/html; charset=utf-8\" />\n")
	if !o.Created.IsZero() {
		fmt.Fprintf(&out, "<meta name=\"created\" content=\"%s\" />\n", o.Created.Format("2006-01-02T15:04:05-07:00"))
	}
	out.WriteString("</head>\n<body>\n")
	out.WriteString(o.BodyXHTML())
	out.WriteString("</body>\n</html>\n")
	return out.String()
}

// BodyXHTML renders only the blocks, as the content of a body element. A
// target appends it to an existing page.
func (o Outline) BodyXHTML() string {
	var out strings.Builder
	for _, block := range o.Blocks {
		switch block.Kind {
		case BlockHeading:
			level := clampLevel(block.Level)
			fmt.Fprintf(&out, "<h%d>%s</h%d>\n", level, xmlText(block.Text), level)
		case BlockParagraph:
			if block.Emphasis {
				fmt.Fprintf(&out, "<p><i>%s</i></p>\n", xmlText(block.Text))
			} else {
				fmt.Fprintf(&out, "<p>%s</p>\n", xmlText(block.Text))
			}
		case BlockList:
			tag := "ul"
			if block.Ordered {
				tag = "ol"
			}
			fmt.Fprintf(&out, "<%s>\n", tag)
			for _, item := range block.Items {
				lines := strings.Split(cleanText(item), "\n")
				if len(lines) == 1 {
					fmt.Fprintf(&out, "<li>%s</li>\n", xmlText(lines[0]))
					continue
				}
				out.WriteString("<li>")
				for _, line := range lines {
					fmt.Fprintf(&out, "<p>%s</p>", xmlText(line))
				}
				out.WriteString("</li>\n")
			}
			fmt.Fprintf(&out, "</%s>\n", tag)
		case BlockImage:
			fmt.Fprintf(&out, "<img src=\"name:%s\" alt=\"%s\" />\n", ImagePartName(block.ImageID), xmlText(block.Alt))
		}
	}
	return out.String()
}

func clampLevel(level int) int {
	return min(max(level, 1), 6)
}

// xmlText escapes text for an element or a quoted attribute.
func xmlText(value string) string {
	return html.EscapeString(cleanText(value))
}

// cleanText makes text valid UTF-8 and drops the control characters XML 1.0
// cannot carry, keeping tab and newline.
func cleanText(value string) string {
	value = strings.ToValidUTF8(value, string(utf8.RuneError))
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n':
			return r
		case r == '\r':
			return '\n'
		case r < 0x20, r == 0xFFFE, r == 0xFFFF:
			return -1
		default:
			return r
		}
	}, value)
}

// languageAttr returns a BCP-47 tag safe for an attribute, or "".
func languageAttr(locale string) string {
	locale = strings.TrimSpace(locale)
	for _, r := range locale {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return ""
		}
	}
	return locale
}
