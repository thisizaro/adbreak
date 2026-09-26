// Package manifest emits the IAB VMAP 1.0 ad-break schedule with inline VAST 3.0
// responses (VASTAdData), so one document fully describes every break.
package manifest

import (
	"encoding/xml"
	"fmt"
	"math"
)

type Ad struct {
	BreakID    string
	Offset     float64 // seconds into content
	AdID       string
	Title      string
	Seconds    float64
	MediaURL   string
	Width      int
	Height     int
	Impression string
}

type vmap struct {
	XMLName xml.Name  `xml:"vmap:VMAP"`
	NS      string    `xml:"xmlns:vmap,attr"`
	Version string    `xml:"version,attr"`
	Breaks  []adBreak `xml:"vmap:AdBreak"`
}

type adBreak struct {
	TimeOffset string   `xml:"timeOffset,attr"`
	BreakType  string   `xml:"breakType,attr"`
	BreakID    string   `xml:"breakId,attr"`
	Source     adSource `xml:"vmap:AdSource"`
}

type adSource struct {
	ID               string `xml:"id,attr"`
	AllowMultipleAds bool   `xml:"allowMultipleAds,attr"`
	FollowRedirects  bool   `xml:"followRedirects,attr"`
	Data             struct {
		VAST VAST `xml:"VAST"`
	} `xml:"vmap:VASTAdData"`
}

type VAST struct {
	XMLName xml.Name `xml:"VAST"`
	Version string   `xml:"version,attr"`
	Ads     []vastAd `xml:"Ad"`
}

type vastAd struct {
	ID     string `xml:"id,attr"`
	InLine struct {
		AdSystem   string `xml:"AdSystem"`
		AdTitle    string `xml:"AdTitle"`
		Impression cdata  `xml:"Impression"`
		Creatives  struct {
			Creative []struct {
				ID     string `xml:"id,attr"`
				Linear struct {
					Duration   string `xml:"Duration"`
					MediaFiles struct {
						MediaFile []mediaFile `xml:"MediaFile"`
					} `xml:"MediaFiles"`
				} `xml:"Linear"`
			} `xml:"Creative"`
		} `xml:"Creatives"`
	} `xml:"InLine"`
}

type mediaFile struct {
	Delivery string `xml:"delivery,attr"`
	Type     string `xml:"type,attr"`
	Width    int    `xml:"width,attr"`
	Height   int    `xml:"height,attr"`
	URL      string `xml:",cdata"`
}

type cdata string

func (c cdata) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	return e.EncodeElement(struct {
		S string `xml:",cdata"`
	}{string(c)}, start)
}

// Clock formats seconds as HH:MM:SS.mmm (VMAP timeOffset) or HH:MM:SS when ms is false (VAST Duration).
func Clock(sec float64, ms bool) string {
	total := int64(math.Round(sec * 1000))
	h, m, s, milli := total/3600000, total/60000%60, total/1000%60, total%1000
	if ms {
		return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, milli)
	}
	if milli >= 500 {
		s++
	}
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func vastFor(a Ad) VAST {
	var ad vastAd
	ad.ID = a.AdID
	ad.InLine.AdSystem = "adbreak"
	ad.InLine.AdTitle = a.Title
	ad.InLine.Impression = cdata(a.Impression)
	ad.InLine.Creatives.Creative = make([]struct {
		ID     string `xml:"id,attr"`
		Linear struct {
			Duration   string `xml:"Duration"`
			MediaFiles struct {
				MediaFile []mediaFile `xml:"MediaFile"`
			} `xml:"MediaFiles"`
		} `xml:"Linear"`
	}, 1)
	c := &ad.InLine.Creatives.Creative[0]
	c.ID = a.AdID
	c.Linear.Duration = Clock(a.Seconds, false)
	c.Linear.MediaFiles.MediaFile = []mediaFile{{Delivery: "progressive", Type: "video/mp4", Width: a.Width, Height: a.Height,
		URL: a.MediaURL}}
	return VAST{Version: "3.0", Ads: []vastAd{ad}}
}

// VMAP renders the schedule. Ads must be sorted by offset.
func VMAP(ads []Ad) ([]byte, error) {
	doc := vmap{NS: "http://www.iab.net/videosuite/vmap", Version: "1.0"}
	for _, a := range ads {
		b := adBreak{TimeOffset: Clock(a.Offset, true), BreakType: "linear", BreakID: a.BreakID}
		b.Source.ID = a.AdID
		b.Source.FollowRedirects = true
		b.Source.Data.VAST = vastFor(a)
		doc.Breaks = append(doc.Breaks, b)
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}
