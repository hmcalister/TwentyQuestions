package main

import (
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"

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
	// Logging Setup
	// --------------------------------------------------------------------------------

	logFileHandle := &lumberjack.Logger{
		Filename: "./logs/log",
		MaxSize:  100,
		MaxAge:   31,
		Compress: true,
	}

	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log.Logger = log.
		With().Caller().Logger().
		With().Timestamp().Logger()

	log.Logger = log.Output(logFileHandle)
	if *debugFlag {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)

		consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout}
		multiWriter := zerolog.MultiLevelWriter(consoleWriter, logFileHandle)
		log.Logger = log.Output(multiWriter)
	}

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
