package native

import (
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// skippedAtoms are elements whose content is never text for the reader.
var skippedAtoms = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Svg: true, atom.Iframe: true, atom.Head: true, atom.Nav: true, atom.Footer: true,
}

// blockAtoms start and end on their own line.
var blockAtoms = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Header: true, atom.Ul: true, atom.Ol: true, atom.Table: true, atom.Tr: true,
	atom.Blockquote: true, atom.Br: true, atom.Hr: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
	atom.Figure: true, atom.Figcaption: true, atom.Form: true,
}

var headingLevel = map[atom.Atom]int{atom.H1: 1, atom.H2: 2, atom.H3: 3, atom.H4: 4, atom.H5: 5, atom.H6: 6}

var blankLines = regexp.MustCompile(`\n{3,}`)
var inlineSpace = regexp.MustCompile(`[ \t\r\f\v]+`)

// htmlToText extracts the title and a markdown-ish text of an HTML page:
// headings as "#", links as [text](href), pre blocks fenced, list items as
// "- ". Scripts, styles and navigation are dropped; relative links are
// resolved against base.
func htmlToText(src string, base *url.URL) (title, text string) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", src
	}
	title = findTitle(doc)
	var b strings.Builder
	var walk func(n *html.Node, inPre bool)
	walk = func(n *html.Node, inPre bool) {
		switch n.Type {
		case html.TextNode:
			if inPre {
				b.WriteString(n.Data)
				return
			}
			b.WriteString(inlineSpace.ReplaceAllString(strings.ReplaceAll(n.Data, "\n", " "), " "))
			return
		case html.ElementNode:
			if skippedAtoms[n.DataAtom] {
				return
			}
			switch {
			case n.DataAtom == atom.Pre:
				b.WriteString("\n```\n")
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, true)
				}
				b.WriteString("\n```\n")
				return
			case n.DataAtom == atom.Code && !inPre:
				b.WriteString("`")
				defer b.WriteString("`")
			case headingLevel[n.DataAtom] > 0:
				b.WriteString("\n\n" + strings.Repeat("#", headingLevel[n.DataAtom]) + " ")
				defer b.WriteString("\n\n")
			case n.DataAtom == atom.Li:
				b.WriteString("\n- ")
			case n.DataAtom == atom.Td || n.DataAtom == atom.Th:
				b.WriteString(" | ")
			case n.DataAtom == atom.A:
				if href := attr(n, "href"); href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "javascript:") {
					// Relative links are made absolute so they can be fetched next.
					if base != nil {
						if ref, err := base.Parse(href); err == nil {
							href = ref.String()
						}
					}
					b.WriteString("[")
					defer b.WriteString("](" + href + ")")
				}
			case blockAtoms[n.DataAtom]:
				b.WriteString("\n")
				defer b.WriteString("\n")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inPre)
		}
	}
	walk(doc, false)
	// Trim each line, then collapse runs of blank lines.
	lines := strings.Split(b.String(), "\n")
	inFence := false
	for i, l := range lines {
		if strings.TrimSpace(l) == "```" {
			inFence = !inFence
		}
		if !inFence {
			lines[i] = strings.TrimSpace(l)
		}
	}
	text = blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return title, strings.TrimSpace(text)
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// findTitle returns the text of the first <title> element.
func findTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.DataAtom == atom.Title {
		if n.FirstChild != nil {
			return strings.TrimSpace(n.FirstChild.Data)
		}
		return ""
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := findTitle(c); t != "" {
			return t
		}
	}
	return ""
}
