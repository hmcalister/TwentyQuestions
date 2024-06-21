package main

import (
	"flag"
	"html/template"
var (
	indexTemplate    = template.Must(template.New("index.html").ParseFiles("templates/index.html"))
)

func main() {
	// --------------------------------------------------------------------------------
	// Flags
	// --------------------------------------------------------------------------------

	port := flag.Int("port", 3000, "The port to use for the HTTP server.")
	debugFlag := flag.Bool("debug", false, "Flag for debug level with console log outputs.")
	flag.Parse()

}
