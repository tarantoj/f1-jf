// Command f1m3u8 lists the live F1 streams from the F1Net dashboard and
// prints a playable m3u8 URL per source.
//
// Usage:
//
//	f1m3u8 [-quality 1080p] [-verify]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	f1net "f1-jf/internal/f1net"
)

func main() {
	var (
		quality = flag.String("quality", "", "requested quality (540p/720p/1080p/2160p); empty = auto")
		verify  = flag.Bool("verify", false, "GET each playlist to confirm it is reachable")
	)
	flag.Parse()

	c := f1net.Client{VerifyPlaylist: *verify}

	sources, err := c.ListSources(context.Background())
	if err != nil {
		log.Fatalf("list sources: %v", err)
	}
	fmt.Printf("found %d sources\n\n", len(sources))
	for _, s := range sources {
		fmt.Printf("  %-12s %s\n", s.Name, s.URL)
	}

	streams, err := c.ResolveAll(context.Background(), *quality)
	if err != nil {
		log.Fatalf("resolve streams: %v", err)
	}
	fmt.Printf("\nresolved %d stream(s)\n", len(streams))
	for _, st := range streams {
		fmt.Printf("\n%s\n%s\n", st.String(), "  ffplay:  "+st.PlaylistURL)
	}

	if len(streams) == 0 {
		os.Exit(1)
	}
}
