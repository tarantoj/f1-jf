package epg

import (
	"bytes"
	"encoding/xml"
	"time"

	"github.com/sherif-fanous/xmltv"
)

// xmltvDoc renders a full XMLTV document for the given channels. Every
// programme is listed under every channel because they all carry the same
// live feed.
func xmltvDoc(channels []xmltvChannel, programmes []xmltvProgramme) []byte {
	tv := xmltv.TV{
		GeneratorInfoName: ptr("f1-jf"),
		Channels:          make([]xmltv.Channel, 0, len(channels)),
		Programmes:        make([]xmltv.Programme, 0, len(channels)*len(programmes)),
	}
	for _, ch := range channels {
		tv.Channels = append(tv.Channels, xmltv.Channel{
			ID: ch.ID,
			DisplayNames: []xmltv.DisplayName{
				{Lang: ptr("en"), Text: ch.Name},
			},
		})
	}
	for _, p := range programmes {
		for _, ch := range channels {
			tv.Programmes = append(tv.Programmes, xmltv.Programme{
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
			})
		}
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

type xmltvChannel struct {
	ID   string
	Name string
}

type xmltvProgramme struct {
	Start time.Time
	Stop  time.Time
	Title string
	Desc  string
}

// ptr returns a pointer to v, for the library's pointer-typed fields.
func ptr[T any](v T) *T { return &v }
