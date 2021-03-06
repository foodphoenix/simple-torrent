package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

type rssJSONItem struct {
	Name            string `json:"name,omitempty"`
	Magnet          string `json:"magnet,omitempty"`
	InfoHash        string `json:"hashinfo,omitempty"`
	Published       string `json:"published,omitempty"`
	URL             string `json:"url,omitempty"`
	publishedParsed *time.Time
}

func (s *Server) updateRSS() {
	fp := gofeed.NewParser()
	fp.Client = &http.Client{
		Timeout: 60 * time.Second,
	}
	for _, rss := range strings.Split(s.state.Config.RssURL, "\n") {
		if !strings.HasPrefix(rss, "http://") && !strings.HasPrefix(rss, "https://") {
			continue
		}
		rss = strings.TrimSpace(rss)
		feed, err := fp.ParseURL(rss)
		if err != nil {
			log.Printf("parse feed err %s", err.Error())
			continue
		}

		if s.Debug {
			log.Printf("retrived feed %s from %s", feed.Title, rss)
		}

		s.state.Lock()
		if oldmark, ok := s.state.rssMark[rss]; ok {
			var lastIdx int
			for i, item := range feed.Items {
				if item.GUID == oldmark {
					lastIdx = i
					break
				}
			}
			if lastIdx > 0 {
				log.Printf("feed updated with %d new items", lastIdx)
				s.state.rssMark[rss] = feed.Items[0].GUID
				s.state.rssCache = append(feed.Items[:lastIdx], s.state.rssCache...)
			}
		} else if len(feed.Items) > 0 {
			if s.Debug {
				log.Printf("retrive %d new items, first record", len(feed.Items))
			}
			s.state.rssMark[rss] = feed.Items[0].GUID
			s.state.rssCache = append(feed.Items, s.state.rssCache...)
		}

		if len(s.state.rssCache) > 200 {
			s.state.rssCache = s.state.rssCache[:200]
		}
		s.state.Unlock()
	}

	s.state.Lock()
	// sort the retrived feed by Published attr
	// make sure the first is the latest
	sort.Slice(s.state.rssCache, func(i, j int) bool {
		return s.state.rssCache[i].PublishedParsed.After(*(s.state.rssCache[j].PublishedParsed))
	})
	if len(s.state.rssCache) > 0 {
		s.state.LatestRSSGuid = s.state.rssCache[0].GUID
	}
	s.state.Unlock()
}

func (s *Server) serveRSS(w http.ResponseWriter, r *http.Request) {

	if _, ok := r.URL.Query()["update"]; ok {
		s.updateRSS()
	}

	var results []rssJSONItem
	for _, i := range s.state.rssCache {
		ritem := rssJSONItem{
			Name:            i.Title,
			Published:       i.Published,
			URL:             i.Link,
			publishedParsed: i.PublishedParsed,
		}

		// not sure which is the the torrent feed standard
		// here is how to get magnet from struct of https://eztv.io/ezrss.xml
		if etor, ok := i.Extensions["torrent"]; ok {
			if e, ok := etor["magnetURI"]; ok && len(e) > 0 {
				ritem.Magnet = e[0].Value
			}
			if e, ok := etor["infoHash"]; ok && len(e) > 0 {
				ritem.InfoHash = e[0].Value
			}
		} else {
			// some sites put it under enclosures
			for _, e := range i.Enclosures {
				if strings.HasPrefix(e.URL, "magnet:") {
					ritem.Magnet = e.URL
				}
			}

			if ritem.Magnet == "" {
				ritem.Magnet = i.Link
			}
		}
		results = append(results, ritem)
	}

	b, err := json.Marshal(results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(b)
}
