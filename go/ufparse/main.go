package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"willnorris.com/go/microformats"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("Usage: ufparse <URL>")
	}
	u, err := url.Parse(os.Args[1])
	if err != nil {
		return
	}
	resp, err := http.Get(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to get url: %s", err)
	}
	data := microformats.Parse(resp.Body, u)
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Failed to encode JSON: %s", err)
	}
	fmt.Printf("%s\n", string(b))
}
