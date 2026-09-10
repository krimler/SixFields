package main

import (
	"fmt"
	"os"

	"capi-distro/internal/fixture"
)

func main() {
	for _, tl := range fixture.All() {
		if err := fixture.Write("testdata/fixtures", tl); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%-22s %d envelopes\n", tl.Name, len(tl.Envelopes))
	}
}
