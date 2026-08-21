package epg

import (
	"bytes"
	"encoding/xml"

	"github.com/sherif-fanous/xmltv"

	"f1-jf/internal/iptv"
)

// xmltvDoc renders a full XMLTV document for the channel. Every programme is
// listed under the channel's id. The channel icon comes from the channel's
// configured Logo (F1IPTV_CHANNEL_LOGO), falling back to the next upcoming
// programme's circuit image.
func xmltvDoc(ch *iptv.Channel, programmes []Programme) []byte {
	logo := ch.Logo
	if logo == "" {
		logo = nextImage(programmes)
	}

	tv := xmltv.TV{
		GeneratorInfoName: ptr("f1-jf"),
		Channels: []xmltv.Channel{
			{
				ID: ch.ID,
				DisplayNames: []xmltv.DisplayName{
					{Lang: ptr("en"), Text: ch.Name},
				},
			},
		},
		Programmes: make([]xmltv.Programme, 0, len(programmes)),
	}
	if logo != "" {
		tv.Channels[0].Icons = []xmltv.Icon{{Source: logo}}
	}
	for _, p := range programmes {
		prog := xmltv.Programme{
			Start:   xmltv.Time{Time: p.Start},
			Stop:    &xmltv.Time{Time: p.Stop},
			Channel: ch.ID,
			Titles: []xmltv.Title{
				{Lang: ptr("en"), Text: p.Title},
			},
			Descriptions: []xmltv.Description{
				{Lang: ptr("en"), Text: p.Desc},
			},
			Categories: []xmltv.Category{
				{Lang: ptr("en"), Text: "Sport"},
			},
		}
		if p.Image != "" {
			prog.Icons = []xmltv.Icon{{Source: p.Image}}
		}
		tv.Programmes = append(tv.Programmes, prog)
	}

	var b bytes.Buffer
	b.WriteString(xml.Header)
	enc := xml.NewEncoder(&b)
	enc.Indent("", "  ")
	if err := enc.Encode(tv); err != nil {
		return []byte(xml.Header + "</tv>\n")
	}
	return b.Bytes()
}

// nextImage returns the image of the first programme that carries one.
func nextImage(programmes []Programme) string {
	for _, p := range programmes {
		if p.Image != "" {
			return p.Image
		}
	}
	return ""
}

// ptr returns a pointer to v, for the library's pointer-typed fields.
func ptr[T any](v T) *T { return &v }
