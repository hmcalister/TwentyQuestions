package main

import (
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
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

	// --------------------------------------------------------------------------------
	// Router and Middlewares
	// --------------------------------------------------------------------------------

	router := chi.NewRouter()
	// --------------------------------------------------------------------------------
	// Serve
	// --------------------------------------------------------------------------------

	targetBindAddress := fmt.Sprintf("localhost:%v", *port)
	log.Info().Msgf("Starting server on %v", targetBindAddress)
	err := http.ListenAndServe(targetBindAddress, router)
	if err != nil {
		log.Fatal().Err(err).Msg("Error during http listen and serve")
	}
}
