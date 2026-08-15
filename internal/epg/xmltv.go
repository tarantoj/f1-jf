package epg

import (
	"encoding/xml"
	"strings"
)

// xmltvTime is the XMLTV timestamp layout: YYYYMMDDHHMMSS ±HHMM.
const xmltvTime = "20060102150405 -0700"

// xmltvDoc renders a full XMLTV document for the given channels. Every
// programme is listed under every channel because they all carry the same
// live feed.
func xmltvDoc(channels []xmltvChannel, programmes []xmltvProgramme) []byte {
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString("<tv generator-info-name=\"f1-jf\">\n")
	for _, ch := range channels {
		b.WriteString(`  <channel id="`)
		b.WriteString(ch.ID)
		b.WriteString("\">\n")
		b.WriteString(`    <display-name lang="en">`)
		b.WriteString(escapeXML(ch.Name))
		b.WriteString("</display-name>\n")
		b.WriteString("  </channel>\n")
	}
	for _, p := range programmes {
		for _, ch := range channels {
			b.WriteString(`  <programme start="`)
			b.WriteString(p.Start)
			b.WriteString(`" stop="`)
			b.WriteString(p.Stop)
			b.WriteString(`" channel="`)
			b.WriteString(ch.ID)
			b.WriteString("\">\n")
			b.WriteString(`    <title lang="en">`)
			b.WriteString(escapeXML(p.Title))
			b.WriteString("</title>\n")
			if p.Desc != "" {
				b.WriteString(`    <desc lang="en">`)
				b.WriteString(escapeXML(p.Desc))
				b.WriteString("</desc>\n")
			}
			b.WriteString("    <category lang=\"en\">Sport</category>\n")
			b.WriteString("  </programme>\n")
		}
	}
	b.WriteString("</tv>\n")
	return []byte(b.String())
}

type xmltvChannel struct {
	ID   string
	Name string
}

type xmltvProgramme struct {
	Start string
	Stop  string
	Title string
	Desc  string
}

// escapeXML escapes the five XML special characters.
func escapeXML(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
